package assist

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"vectro/cloud/internal/clad/cloud"
	"vectro/cloud/internal/log"
	"vectro/cloud/internal/voice"
	"vectro/cloud/internal/wyoming"
)

const (
	micRate = 16000
	// workTimeout bounds one request from wake word to spoken reply.
	workTimeout = 30 * time.Second
	dialTimeout = 3 * time.Second
	// ackIntent ends the engine's voice interaction for requests that go to
	// Home Assistant: Vector gives a happy acknowledgement, then speaks HA's
	// reply with SayText. (The engine's knowledge-graph answer behavior isn't
	// always allowed to run, which left the intent unclaimed and his voice
	// handling stuck.) Vector's own commands get their native intent instead.
	ackIntent = "intent_imperative_affirmative"
)

// Speaker makes Vector say something in his own voice.
type Speaker interface {
	Say(ctx context.Context, text string) error
}

// Backend is a voice.Backend that uses Whisper and Home Assistant.
type Backend struct {
	cfg     Config
	http    *http.Client
	speaker Speaker
	sayMu   sync.Mutex // one reply at a time
}

// New creates a Backend that speaks replies through speaker.
func New(cfg Config, speaker Speaker) *Backend {
	return &Backend{cfg: cfg, http: &http.Client{Timeout: 15 * time.Second}, speaker: speaker}
}

// Open implements voice.Backend.
func (b *Backend) Open(ctx context.Context, req voice.Request, sink voice.Sink) (voice.Session, error) {
	if req.ConnectionCheck {
		go b.connectionCheck(ctx, sink)
		return noopSession{}, nil
	}

	// The request's work isn't tied to ctx: the voice process closes the
	// session once we acknowledge, but transcription and the reply carry on.
	work, cancel := context.WithTimeout(context.Background(), workTimeout)
	s := &session{b: b, sink: sink, ctx: ctx, work: work, cancel: cancel, audio: make(chan []int16, 64), opened: time.Now(), vad: newEndpointer(micRate)}

	go s.deliver(func() { sink.OnStreamOpen("vectro") })

	var d net.Dialer
	dctx, dcancel := context.WithTimeout(work, dialTimeout)
	conn, err := d.DialContext(dctx, "tcp", b.cfg.Whisper)
	dcancel()
	if err != nil {
		log.Println("vectro: can't reach speech-to-text at", b.cfg.Whisper+":", err)
		go func() {
			for range s.audio { // discard the utterance
			}
		}()
		s.reply(&cloud.IntentResult{Intent: ackIntent})
		go s.speak("I can't reach the speech server.")
		return s, nil
	}
	go func() {
		<-work.Done()
		conn.Close()
	}()
	go s.run(conn)
	return s, nil
}

func (b *Backend) connectionCheck(ctx context.Context, sink voice.Sink) {
	code, status := cloud.ConnectionCode_Available, "vectro: Whisper and Home Assistant reachable"
	if c, err := net.DialTimeout("tcp", b.cfg.Whisper, dialTimeout); err != nil {
		code, status = cloud.ConnectionCode_Connectivity, "vectro: speech-to-text unreachable"
	} else {
		c.Close()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, b.cfg.HAURL+"/api/", nil)
		req.Header.Set("Authorization", "Bearer "+b.cfg.HAToken)
		resp, err := b.http.Do(req)
		switch {
		case err != nil:
			code, status = cloud.ConnectionCode_Connectivity, "vectro: Home Assistant unreachable"
		case resp.StatusCode == http.StatusUnauthorized:
			code, status = cloud.ConnectionCode_Auth, "vectro: Home Assistant token rejected"
		}
		if resp != nil {
			resp.Body.Close()
		}
	}
	expected := uint8(voice.DefaultAudioLenMs / voice.DefaultChunkMs)
	sink.OnConnectionResult(&cloud.ConnectionResult{
		Code: code, Status: status, NumPackets: expected, ExpectedPackets: expected,
	})
}

type noopSession struct{}

func (noopSession) AddSamples([]int16) {}
func (noopSession) CloseSend() error   { return nil }
func (noopSession) Close() error       { return nil }

// session is one utterance: stream it to Whisper, ask HA, speak the reply.
type session struct {
	b    *Backend
	sink voice.Sink
	// ctx is the voice process's; it ends when the session is closed.
	ctx context.Context
	// work bounds transcription and the reply, independent of ctx.
	work   context.Context
	cancel context.CancelFunc

	audio     chan []int16
	stopOnce  sync.Once
	replyOnce sync.Once

	opened time.Time // when the wake word opened the session
	vad    *endpointer

	mu      sync.Mutex
	closed  bool      // audio channel closed; no more samples
	stopped time.Time // when the utterance ended
}

// AddSamples queues mic audio without blocking the voice process, and ends
// the utterance as soon as the speaker pauses.
func (s *session) AddSamples(samples []int16) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	select {
	case s.audio <- samples:
	default:
		log.Println("vectro: dropping mic audio, speech-to-text is behind")
	}
	s.mu.Unlock()

	if s.vad.push(samples) {
		s.CloseSend()
	}
}

// CloseSend marks the end of the utterance. The reply intent follows once
// the transcript shows whether it's one of Vector's own commands.
func (s *session) CloseSend() error {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.stopped = time.Now()
		close(s.audio)
		s.mu.Unlock()
	})
	return nil
}

// Close is called when the voice process is done with the session. Before
// the utterance is complete that means it was abandoned, so stop working.
func (s *session) Close() error {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		close(s.audio)
		s.mu.Unlock()
		s.cancel()
	})
	return nil
}

func (s *session) micStopped() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

func (s *session) run(conn net.Conn) {
	defer s.cancel()
	text, err := transcribe(conn, s.b.cfg.Language, s.audio)
	if err != nil {
		if s.work.Err() == nil {
			log.Println("vectro: speech-to-text failed:", err)
			s.reply(&cloud.IntentResult{Intent: ackIntent})
			s.speak("Sorry, the speech server had a problem.")
		}
		return
	}
	sttDone := time.Now()
	stopped := s.micStopped()
	log.Println(fmt.Sprintf("vectro: heard %q (listened %dms, speech-to-text %dms)",
		text, stopped.Sub(s.opened).Milliseconds(), sttDone.Sub(stopped).Milliseconds()))
	if text == "" {
		s.reply(&cloud.IntentResult{Intent: "intent_system_noaudio"})
		return
	}
	if li, ok := route(text); ok {
		log.Println("vectro: Vector command:", li.Intent, li.Params)
		s.reply(&cloud.IntentResult{Intent: li.Intent, Parameters: li.parameters()})
		return
	}
	s.reply(&cloud.IntentResult{Intent: ackIntent})
	answer, err := s.b.converse(s.work, text)
	if err != nil {
		log.Println("vectro: Home Assistant failed:", err)
		s.speak("Sorry, Home Assistant didn't answer.")
		return
	}
	log.Println(fmt.Sprintf("vectro: replying %q (Home Assistant %dms)", answer, time.Since(sttDone).Milliseconds()))
	s.speak(answer)
}

// speak says text once any earlier reply has finished.
func (s *session) speak(text string) {
	s.b.sayMu.Lock()
	defer s.b.sayMu.Unlock()
	if s.b.speaker == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	start := time.Now()
	if err := s.b.speaker.Say(ctx, text); err != nil {
		log.Println("vectro: couldn't speak reply:", err)
		return
	}
	log.Println(fmt.Sprintf("vectro: spoke reply (%dms incl. speaking; %dms since wake word)",
		time.Since(start).Milliseconds(), time.Since(s.opened).Milliseconds()))
}

// transcribe streams audio to the Wyoming STT service on conn and returns
// its transcript.
func transcribe(conn net.Conn, language string, audio <-chan []int16) (string, error) {
	if err := wyoming.Write(conn, &wyoming.Event{Type: "transcribe", Data: map[string]any{"language": language}}); err != nil {
		return "", err
	}
	if err := wyoming.Write(conn, &wyoming.Event{Type: "audio-start", Data: map[string]any{
		"rate": micRate, "width": 2, "channels": 1, "timestamp": 0,
	}}); err != nil {
		return "", err
	}
	var timestamp int
	for samples := range audio {
		buf := make([]byte, len(samples)*2)
		for i, v := range samples {
			buf[2*i] = byte(v)
			buf[2*i+1] = byte(uint16(v) >> 8)
		}
		if err := wyoming.Write(conn, &wyoming.Event{
			Type:    "audio-chunk",
			Data:    map[string]any{"rate": micRate, "width": 2, "channels": 1, "timestamp": timestamp},
			Payload: buf,
		}); err != nil {
			return "", err
		}
		timestamp += len(samples) * 1000 / micRate
	}
	if err := wyoming.Write(conn, &wyoming.Event{Type: "audio-stop", Data: map[string]any{"timestamp": timestamp}}); err != nil {
		return "", err
	}

	r := bufio.NewReader(conn)
	for {
		ev, err := wyoming.Read(r)
		if err != nil {
			return "", err
		}
		if ev.Type == "transcript" {
			return ev.String("text"), nil
		}
	}
}

// converse sends text to HA's conversation API and returns its spoken reply.
func (b *Backend) converse(ctx context.Context, text string) (string, error) {
	body := map[string]string{"text": text, "language": b.cfg.Language}
	if b.cfg.Agent != "" {
		body["agent_id"] = b.cfg.Agent
	}
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.cfg.HAURL+"/api/conversation/process", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+b.cfg.HAToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %s", resp.Status)
	}
	var out struct {
		Response struct {
			Speech struct {
				Plain struct {
					Speech string `json:"speech"`
				} `json:"plain"`
			} `json:"speech"`
		} `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Response.Speech.Plain.Speech == "" {
		return "", errors.New("empty reply")
	}
	return out.Response.Speech.Plain.Speech, nil
}

func (s *session) reply(r *cloud.IntentResult) {
	s.replyOnce.Do(func() {
		go s.deliver(func() { s.sink.OnIntent(r) })
	})
}

// deliver runs fn unless the voice process has closed the session.
func (s *session) deliver(fn func()) {
	select {
	case <-s.ctx.Done():
		return
	default:
	}
	fn()
}
