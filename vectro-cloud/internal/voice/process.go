package voice

import (
	"context"
	"time"

	"vectro/cloud/internal/clad/cloud"

	"vectro/cloud/internal/log"
)

var (
	verbose bool
)

const (
	// DefaultAudioLenMs is the number of milliseconds of audio we send for connection checks
	DefaultAudioLenMs = 6000
	// DefaultChunkMs is the default value for how often audio is sent to the cloud
	DefaultChunkMs = 120
	// SampleRate defines how many samples per second should be sent
	SampleRate = 16000
	// SampleBits defines how many bits each sample should contain
	SampleBits = 16
	// DefaultTimeout is the length of time before the process will cancel a voice request
	DefaultTimeout = 9 * time.Second
)

// Process contains the data associated with an instance of the cloud process,
// and can have receivers and callbacks associated with it before ultimately
// being started with Run()
type Process struct {
	receivers []*Receiver
	intents   []MsgSender
	kill      chan struct{}
	msg       chan messageEvent
	opts      options
}

// AddReceiver adds the given Receiver to the list of sources the
// cloud process will listen to for data
func (p *Process) AddReceiver(r *Receiver) {
	if p.receivers == nil {
		p.receivers = make([]*Receiver, 0, 4)
		p.msg = make(chan messageEvent)
	}
	if p.kill == nil {
		p.kill = make(chan struct{})
	}
	p.addMultiplexRoutine(r)
	p.receivers = append(p.receivers, r)
}

// AddTestReceiver adds the given Receiver to the list of sources the
// cloud process will listen to for data. Additionally, it will be
// marked as a test receiver, which means that data sent on this
// receiver will send a message to the mic requesting it notify the
// AI of a hotword event on our behalf.
func (p *Process) AddTestReceiver(r *Receiver) {
	r.isTest = true
	p.AddReceiver(r)
}

type messageEvent struct {
	msg    *cloud.Message
	isTest bool
}

func (p *Process) addMultiplexRoutine(r *Receiver) {
	go func() {
		for {
			select {
			case <-p.kill:
				return
			case msg := <-r.msg:
				p.msg <- messageEvent{msg: msg, isTest: r.isTest}
			}
		}
	}()
}

// AddIntentWriter adds the given Writer to the list of writers that will receive
// intents from the cloud
func (p *Process) AddIntentWriter(s MsgSender) {
	if p.intents == nil {
		p.intents = make([]MsgSender, 0, 4)
	}
	p.intents = append(p.intents, s)
}

type strmReceiver struct {
	stream     Session
	intent     chan cloudIntent
	err        chan cloudError
	open       chan cloudOpen
	connection chan cloudConnCheck
}

func (c *strmReceiver) OnIntent(r *cloud.IntentResult) {
	if c.intent == nil {
		log.Println("Unexpected intent result on receiver:", r)
		return
	}
	c.intent <- cloudIntent{c, r}
}

func (c *strmReceiver) OnError(kind cloud.ErrorType, err error) {
	c.err <- cloudError{c, kind, err}
}

func (c *strmReceiver) OnStreamOpen(session string) {
	c.open <- cloudOpen{c, session}
}

func (c *strmReceiver) OnConnectionResult(r *cloud.ConnectionResult) {
	if c.connection == nil {
		log.Println("Unexpected connection check result on receiver:", r)
		return
	}
	c.connection <- cloudConnCheck{c, r}
}

func (c *strmReceiver) Close() {
	if c.intent != nil {
		close(c.intent)
	}
	close(c.err)  // should never be nil
	close(c.open) // should never be nil
	if c.connection != nil {
		close(c.connection)
	}
}

// Run starts the cloud process, which will run until stopped on the given channel
func (p *Process) Run(ctx context.Context, options ...Option) {
	if verbose {
		log.Println("Verbose logging enabled")
	}
	// set default options before processing user options
	p.opts.chunkMs = DefaultChunkMs
	for _, opt := range options {
		opt(&p.opts)
	}

	cloudChans := &strmReceiver{
		intent: make(chan cloudIntent),
		err:    make(chan cloudError),
		open:   make(chan cloudOpen),
	}
	defer cloudChans.Close()

	connCheck := &strmReceiver{
		err:        make(chan cloudError),
		open:       make(chan cloudOpen),
		connection: make(chan cloudConnCheck),
	}
	defer connCheck.Close()

	if p.opts.backend == nil {
		p.opts.backend = &NullBackend{}
	}

	var strm Session
procloop:
	for {
		// the cases in this select should NOT block! if messages that others send us
		// are not promptly read, socket buffers can fill up and break voice processing
		select {
		case msg := <-p.msg:
			switch msg.msg.Tag() {
			case cloud.MessageTag_Hotword:
				// hotword = get ready to stream data
				if strm != nil {
					log.Println("Got hotword event while already streaming, weird...")
					if err := strm.Close(); err != nil {
						log.Println("Error closing context:")
					}
				}

				// if this is from a test receiver, notify the mic to send the AI a hotword on our behalf
				if msg.isTest {
					p.writeMic(cloud.NewMessageWithTestStarted(&cloud.Void{}))
				}

				hw := msg.msg.GetHotword()
				locale := hw.Locale
				if locale == "" {
					locale = "en-US"
				}
				logVerbose("Got hotword event, mode", hw.Mode)
				strm = p.newStream(ctx, cloudChans, Request{
					Mode:      hw.Mode,
					Locale:    locale,
					Timezone:  hw.Timezone,
					NoLogging: hw.NoLogging,
				})

			case cloud.MessageTag_DebugFile:
				p.writeResponse(msg.msg)

			case cloud.MessageTag_AudioDone:
				// no more audio is coming - close send on the stream
				if strm != nil {
					logVerbose("Got notification mic is done sending audio")
					if err := strm.CloseSend(); err != nil {
						log.Println("Error closing stream send:", err)
					}
				}

			case cloud.MessageTag_Audio:
				// add samples to our buffer
				buf := msg.msg.GetAudio().Data
				if strm != nil {
					strm.AddSamples(buf)
				} else {
					logVerbose("No active context, discarding", len(buf), "samples")
				}

			case cloud.MessageTag_ConnectionCheck:
				logVerbose("Got connection check request")
				// connection check = open a stream to check connection quality
				if strm != nil {
					log.Println("Got connection check request while already streaming, closing current stream")
					if err := strm.Close(); err != nil {
						log.Println("Error closing context:")
					}
				}

				strm = p.newStream(ctx, connCheck, Request{ConnectionCheck: true})
			}

		case intent := <-cloudChans.intent:
			if intent.recvr.stream != strm {
				log.Println("Ignoring result from prior stream:", intent.result)
				continue
			}
			logVerbose("Received intent from cloud:", intent.result)

			// we got an answer from the cloud, tell mic to stop...
			p.signalMicStop()

			// send intent to AI
			p.writeResponse(cloud.NewMessageWithResult(intent.result))

			// stop streaming until we get another hotword event
			if err := strm.Close(); err != nil {
				log.Println("Error closing context:")
			}
			strm = nil

		case err := <-cloudChans.err:
			if err.recvr.stream != strm {
				log.Println("Ignoring error from prior stream:", err.err)
				continue
			}
			logVerbose("Received error from cloud:", err.err)
			p.signalMicStop()
			p.writeError(err.kind, err.err)
			if p.opts.errListener != nil {
				p.opts.errListener.OnError(err.err)
			}
			if err := strm.Close(); err != nil {
				log.Println("Error closing context:")
			}
			strm = nil

		case open := <-cloudChans.open:
			if open.recvr.stream != strm {
				log.Println("Ignoring stream open from prior stream:", open.session)
				continue
			}
			p.writeResponse(cloud.NewMessageWithStreamOpen(&cloud.StreamOpen{Session: open.session}))

		case err := <-connCheck.err:
			if err.recvr.stream != strm {
				log.Println("Ignoring error from prior connection check:", err)
				continue
			}
			logVerbose("Received error from conn check:", err)
			p.respondToConnectionCheck(nil, &err)
			if err := strm.Close(); err != nil {
				log.Println("Error closing context:")
			}
			strm = nil

		case <-connCheck.open:
			// don't care

		case r := <-connCheck.connection:
			if r.recvr.stream != strm {
				log.Println("Ignoring connection result from prior check:", r.result)
				continue
			}
			logVerbose("Received connection check result from cloud:", r.result)
			p.respondToConnectionCheck(r.result, nil)
			if err := strm.Close(); err != nil {
				log.Println("Error closing context:")
			}
			strm = nil

		case <-ctx.Done():
			logVerbose("Received stop notification")
			if p.kill != nil {
				close(p.kill)
			}
			break procloop
		}
	}
}

// ChunkSamples is the number of samples that should be in each chunk
func (p *Process) ChunkSamples() int {
	return SampleRate * int(p.opts.chunkMs) / 1000
}

// StreamSize is the size in bytes of each chunk
func (p *Process) StreamSize() int {
	return p.ChunkSamples() * (SampleBits / 8)
}

// SetVerbose enables or disables verbose logging
func SetVerbose(value bool) {
	verbose = value
}

// newStream opens a backend session. Results come back on a copy of receiver
// whose stream field identifies the session, so results from superseded
// sessions can be ignored.
func (p *Process) newStream(ctx context.Context, receiver *strmReceiver, req Request) Session {
	newReceiver := *receiver
	session, err := p.opts.backend.Open(ctx, req, &newReceiver)
	if err != nil {
		log.Println("Voice backend failed to open session:", err)
		if req.ConnectionCheck {
			p.respondToConnectionCheck(nil, &cloudError{kind: cloud.ErrorType_Connecting, err: err})
		} else {
			p.signalMicStop()
			p.writeError(cloud.ErrorType_Connecting, err)
		}
		return nil
	}
	newReceiver.stream = session
	return session
}

func (p *Process) writeError(reason cloud.ErrorType, err error) {
	p.writeResponse(cloud.NewMessageWithError(&cloud.IntentError{Error: reason, Extra: err.Error()}))
}

func (p *Process) writeResponse(response *cloud.Message) {
	for _, r := range p.intents {
		err := r.Send(response)
		if err != nil {
			log.Println("AI write error:", err)
		}
	}
}

func (p *Process) signalMicStop() {
	p.writeMic(cloud.NewMessageWithStopSignal(&cloud.Void{}))
}

func (p *Process) writeMic(msg *cloud.Message) {
	for _, r := range p.receivers {
		err := r.writeBack(msg)
		if err != nil {
			log.Println("Mic write error:", err)
		}
	}
}

func (p *Process) respondToConnectionCheck(result *cloud.ConnectionResult, cErr *cloudError) {
	toSend := &cloud.ConnectionResult{
		NumPackets:      uint8(0),
		ExpectedPackets: uint8(DefaultAudioLenMs / DefaultChunkMs),
	}
	if cErr != nil {
		toSend.Status = cErr.err.Error()
		switch cErr.kind {
		case cloud.ErrorType_TLS:
			toSend.Code = cloud.ConnectionCode_Tls
		case cloud.ErrorType_Connectivity:
			toSend.Code = cloud.ConnectionCode_Connectivity
		case cloud.ErrorType_Timeout:
			toSend.Code = cloud.ConnectionCode_Bandwidth
		case cloud.ErrorType_Connecting:
			fallthrough
		case cloud.ErrorType_InvalidConfig:
			fallthrough
		default:
			toSend.Code = cloud.ConnectionCode_Auth
		}
	} else {
		toSend = result
	}
	p.writeMic(cloud.NewMessageWithConnectionResult(toSend))
}

func logVerbose(a ...interface{}) {
	if !verbose {
		return
	}
	log.Println(a...)
}

type cloudError struct {
	recvr *strmReceiver
	kind  cloud.ErrorType
	err   error
}

type cloudIntent struct {
	recvr  *strmReceiver
	result *cloud.IntentResult
}

type cloudOpen struct {
	recvr   *strmReceiver
	session string
}

type cloudConnCheck struct {
	recvr  *strmReceiver
	result *cloud.ConnectionResult
}
