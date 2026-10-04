package assist

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// localIntent is one of Vector's own voice commands, recognized locally so
// it runs his built-in behavior instead of going to Home Assistant.
type localIntent struct {
	Intent string
	Params map[string]string
}

// parameters encodes Params the way DDL's cloud sent them.
func (l localIntent) parameters() string {
	if len(l.Params) == 0 {
		return ""
	}
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(l.Params)
	return buf.String()
}

type rule struct {
	re     *regexp.Regexp
	intent func(m []string) (localIntent, bool)
}

// fixed returns a rule result with no parameters.
func fixed(intent string) func([]string) (localIntent, bool) {
	return func([]string) (localIntent, bool) { return localIntent{Intent: intent}, true }
}

// Patterns match the whole normalized utterance (see normalize), so Home
// Assistant commands like "turn on the light" never match "turn left".
var rules = []rule{
	// Names and faces
	{re(`(my name is|call me) (.+)`), func(m []string) (localIntent, bool) {
		name := titleCase(m[2])
		if name == "" || len(strings.Fields(name)) > 3 {
			return localIntent{}, false
		}
		return localIntent{Intent: "intent_names_username_extend", Params: map[string]string{"username": name}}, true
	}},
	{re(`(what is|whats) my name|who am i|do you (know|remember) (me|my name|who i am)`), fixed("intent_names_ask")},

	// Timers
	{re(`set (a |the )?timer (for )?(.+)`), func(m []string) (localIntent, bool) {
		secs, ok := parseDuration(m[3])
		if !ok {
			return localIntent{}, false
		}
		return localIntent{Intent: "intent_clock_settimer_extend", Params: map[string]string{"timer_duration": strconv.Itoa(secs)}}, true
	}},
	{re(`(.+) timer`), func(m []string) (localIntent, bool) { // "five minute timer"
		secs, ok := parseDuration(strings.TrimPrefix(strings.TrimPrefix(m[1], "set a "), "a "))
		if !ok {
			return localIntent{}, false
		}
		return localIntent{Intent: "intent_clock_settimer_extend", Params: map[string]string{"timer_duration": strconv.Itoa(secs)}}, true
	}},
	{re(`(check (the |my )?timer|how much time is left|how long is left( on the timer)?)`), fixed("intent_clock_checktimer")},
	{re(`(cancel|stop|delete) (the |my )?timer`), func([]string) (localIntent, bool) {
		return localIntent{Intent: "intent_global_stop_extend", Params: map[string]string{"entity_behavior_stoppable": "timer"}}, true
	}},

	// Photos
	{re(`take (a |my )?(selfie|picture of me|photo of me)`), func([]string) (localIntent, bool) {
		return localIntent{Intent: "intent_photo_take_extend", Params: map[string]string{"entity_photo_selfie": "selfie"}}, true
	}},
	{re(`take (a )?(photo|picture)`), func([]string) (localIntent, bool) {
		return localIntent{Intent: "intent_photo_take_extend", Params: map[string]string{"entity_photo_selfie": ""}}, true
	}},

	// Charger and sleep
	{re(`go (to|back to|home to) (your )?(charger|home|dock|base)|go home|go charge|recharge`), fixed("intent_system_charger")},
	{re(`go to sleep|sleep|take a nap`), fixed("intent_system_sleep")},

	// Movement
	{re(`come (here|to me|over here)`), fixed("intent_imperative_come")},
	{re(`look at me`), fixed("intent_imperative_lookatme")},
	{re(`look (over )?there`), fixed("intent_imperative_lookoverthere")},
	{re(`turn around`), fixed("intent_imperative_turnaround")},
	{re(`turn left`), fixed("intent_imperative_turnleft")},
	{re(`turn right`), fixed("intent_imperative_turnright")},
	{re(`(back up|go back|reverse)`), fixed("intent_imperative_backup")},
	{re(`(go |move )?forward`), fixed("intent_imperative_forward")},
	{re(`(go )?explore|(go )?look around`), fixed("intent_explore_start")},
	{re(`stop`), func([]string) (localIntent, bool) {
		return localIntent{Intent: "intent_global_stop_extend", Params: map[string]string{"entity_behavior_stoppable": ""}}, true
	}},

	// Play and tricks
	{re(`dance|(do a |show me a )?dance`), fixed("intent_imperative_dance")},
	{re(`(give me a |lets )?fist ?bump`), fixed("intent_play_fistbump")},
	{re(`(do |pop )?a wheelie`), fixed("intent_play_popawheelie")},
	{re(`(do a|show me a) trick`), fixed("intent_play_anytrick")},
	{re(`(lets )?play (a game|something)`), fixed("intent_play_anygame")},
	{re(`(lets )?play blackjack`), fixed("intent_play_blackjack")},
	{re(`(hit me|hit)`), fixed("intent_blackjack_hit")},
	{re(`(stand|i stand|stay)`), fixed("intent_blackjack_stand")},
	{re(`(play again|deal again)`), fixed("intent_blackjack_playagain")},
	{re(`(roll|roll your) cube`), fixed("intent_play_rollcube")},
	{re(`pick up (your |the )?cube`), fixed("intent_play_pickupcube")},
	{re(`(find|where is|wheres) (your |the |my )?cube`), fixed("intent_imperative_findcube")},
	{re(`(bring me|fetch) (your |the )?cube`), fixed("intent_imperative_fetchcube")},
	{re(`(play )?keep ?away`), fixed("intent_play_keepaway")},

	// Feelings and manners
	{re(`(good (robot|boy|job|vector)|well done|nice job)`), fixed("intent_imperative_praise")},
	{re(`(bad (robot|boy|vector)|no no)`), fixed("intent_imperative_scold")},
	{re(`(i love you|love you)`), fixed("intent_imperative_love")},
	{re(`(sorry|im sorry|i apologize)`), fixed("intent_imperative_apologize")},
	{re(`(yes|yeah|yep|correct)`), fixed("intent_imperative_affirmative")},
	{re(`(no|nope)`), fixed("intent_imperative_negative")},
	{re(`(be quiet|quiet|hush|shush)`), fixed("intent_imperative_quiet")},
	{re(`shut up`), fixed("intent_imperative_shutup")},
	{re(`(hello|hi|hey|hi there|hello there)`), fixed("intent_greeting_hello")},
	{re(`(goodbye|bye|see you( later)?)`), fixed("intent_greeting_goodbye")},
	{re(`good morning`), fixed("intent_greeting_goodmorning")},
	{re(`good ?night`), fixed("intent_greeting_goodnight")},
	{re(`how are you( doing| feeling)?( today)?`), fixed("intent_status_feeling")},
	{re(`how old are you|whats your age|what is your age`), fixed("intent_character_age")},
	{re(`happy new year`), fixed("intent_seasonal_happynewyear")},
	{re(`(happy holidays|merry christmas)`), fixed("intent_seasonal_happyholidays")},

	// Volume
	{re(`(volume up|louder|turn (it |your volume )?up|speak up)`), fixed("intent_imperative_volumeup")},
	{re(`(volume down|quieter|softer|turn (it |your volume )?down)`), fixed("intent_imperative_volumedown")},
	{re(`(set (your |the )?)?volume (to |at )?(level )?(.+)`), func(m []string) (localIntent, bool) {
		level, ok := parseVolume(m[5])
		if !ok {
			return localIntent{}, false
		}
		return localIntent{Intent: "intent_imperative_volumelevel_extend", Params: map[string]string{"volume_level": level}}, true
	}},

	// Eye color
	{re(`(change|set|make) (your )?eye ?colou?r( to)? (\w+)|(make )?your eyes (\w+)`), func(m []string) (localIntent, bool) {
		word := m[4]
		if word == "" {
			word = m[6]
		}
		color, ok := eyeColors[word]
		if !ok {
			return localIntent{}, false
		}
		return localIntent{Intent: "intent_imperative_eyecolor_specific_extend", Params: map[string]string{"eye_color": color}}, true
	}},
	{re(`change (your )?eye ?colou?rs?`), fixed("intent_imperative_eyecolor")},
}

var eyeColors = map[string]string{
	"teal": "COLOR_TEAL", "orange": "COLOR_ORANGE", "yellow": "COLOR_YELLOW",
	"green": "COLOR_GREEN", "lime": "COLOR_GREEN", "blue": "COLOR_BLUE",
	"purple": "COLOR_PURPLE", "rainbow": "COLOR_RAINBOW",
}

func re(pattern string) *regexp.Regexp {
	return regexp.MustCompile(`^(?:` + pattern + `)$`)
}

// politeness is stripped from both ends before matching.
var (
	leading  = regexp.MustCompile(`^(?:(?:hey |ok |okay )?vector,? |please |can you |could you |would you |will you |i want you to |id like you to |go ahead and )+`)
	trailing = regexp.MustCompile(`(?: please| for me| now| vector| right now)+$`)
)

// normalize lowercases, drops punctuation and apostrophes, collapses
// spaces and removes polite wrapping.
func normalize(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		switch {
		case r == '\'' || r == '’':
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	s := strings.Join(strings.Fields(b.String()), " ")
	for prev := ""; prev != s; {
		prev = s
		s = strings.TrimSpace(trailing.ReplaceAllString(leading.ReplaceAllString(s, ""), ""))
	}
	return s
}

// route returns the local intent for text, if it's one of Vector's own
// commands.
func route(text string) (localIntent, bool) {
	s := normalize(text)
	if s == "" {
		return localIntent{}, false
	}
	for _, r := range rules {
		if m := r.re.FindStringSubmatch(s); m != nil {
			if li, ok := r.intent(m); ok {
				return li, true
			}
		}
	}
	return localIntent{}, false
}

func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

var numberWords = map[string]int{
	"a": 1, "an": 1, "one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7,
	"eight": 8, "nine": 9, "ten": 10, "eleven": 11, "twelve": 12, "thirteen": 13, "fourteen": 14,
	"fifteen": 15, "sixteen": 16, "seventeen": 17, "eighteen": 18, "nineteen": 19, "twenty": 20,
	"thirty": 30, "forty": 40, "fifty": 50, "sixty": 60, "ninety": 90,
}

// parseNumber reads "5", "five", "twenty five" or "a"/"an".
func parseNumber(words []string) (int, bool) {
	if len(words) == 0 {
		return 0, false
	}
	if n, err := strconv.Atoi(words[0]); err == nil && len(words) == 1 {
		return n, true
	}
	total := 0
	for _, w := range words {
		n, ok := numberWords[w]
		if !ok {
			return 0, false
		}
		total += n
	}
	return total, true
}

var durationRe = regexp.MustCompile(`^(?:(.+?) (hours?|hrs?)(?: and)?)? ?(?:(.+?) (minutes?|mins?)(?: and)?)? ?(?:(.+?) (seconds?|secs?))?$`)

// parseDuration reads "5 minutes", "an hour and 10 minutes", "90 seconds",
// "half an hour" and "a minute and a half" into seconds.
func parseDuration(s string) (int, bool) {
	s = strings.TrimSpace(s)
	switch s {
	case "half an hour", "a half hour":
		return 1800, true
	case "a minute and a half", "one and a half minutes", "one minute and a half":
		return 90, true
	}
	m := durationRe.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	total := 0
	found := false
	for _, part := range []struct {
		value string
		mult  int
	}{{m[1], 3600}, {m[3], 60}, {m[5], 1}} {
		if part.value == "" {
			continue
		}
		n, ok := parseNumber(strings.Fields(part.value))
		if !ok {
			return 0, false
		}
		total += n * part.mult
		found = true
	}
	if !found || total <= 0 || total > 24*3600 {
		return 0, false
	}
	return total, true
}

// parseVolume reads "3", "three", "max", "mute" or "low" into VOLUME_n.
func parseVolume(s string) (string, bool) {
	switch s {
	case "mute", "off", "zero", "0":
		return "VOLUME_0", true
	case "low", "minimum", "min", "lowest":
		return "VOLUME_1", true
	case "medium low":
		return "VOLUME_2", true
	case "medium", "middle", "normal":
		return "VOLUME_3", true
	case "medium high":
		return "VOLUME_4", true
	case "high", "max", "maximum", "full", "highest":
		return "VOLUME_5", true
	}
	n, ok := parseNumber(strings.Fields(s))
	if !ok || n < 0 || n > 5 {
		return "", false
	}
	return "VOLUME_" + strconv.Itoa(n), true
}
