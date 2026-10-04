package assist

import "testing"

func TestRouteVectorCommands(t *testing.T) {
	cases := []struct {
		text, intent, param, value string
	}{
		{" My name is jonathan.", "intent_names_username_extend", "username", "Jonathan"},
		{"Call me Mary Jane", "intent_names_username_extend", "username", "Mary Jane"},
		{"What's my name?", "intent_names_ask", "", ""},
		{"Who am I?", "intent_names_ask", "", ""},
		{"Set a timer for 5 minutes.", "intent_clock_settimer_extend", "timer_duration", "300"},
		{"set timer for an hour and 10 minutes", "intent_clock_settimer_extend", "timer_duration", "4200"},
		{"Set a timer for twenty five seconds", "intent_clock_settimer_extend", "timer_duration", "25"},
		{"Five minute timer", "intent_clock_settimer_extend", "timer_duration", "300"},
		{"Set a timer for half an hour", "intent_clock_settimer_extend", "timer_duration", "1800"},
		{"Cancel the timer", "intent_global_stop_extend", "entity_behavior_stoppable", "timer"},
		{"How much time is left?", "intent_clock_checktimer", "", ""},
		{"Take a selfie", "intent_photo_take_extend", "entity_photo_selfie", "selfie"},
		{"Take a picture.", "intent_photo_take_extend", "entity_photo_selfie", ""},
		{"Hey Vector, go to your charger please.", "intent_system_charger", "", ""},
		{"Can you come here?", "intent_imperative_come", "", ""},
		{"Dance!", "intent_imperative_dance", "", ""},
		{"Fist bump", "intent_play_fistbump", "", ""},
		{"Good robot!", "intent_imperative_praise", "", ""},
		{"How are you?", "intent_status_feeling", "", ""},
		{"Set volume to 3", "intent_imperative_volumelevel_extend", "volume_level", "VOLUME_3"},
		{"Volume max", "intent_imperative_volumelevel_extend", "volume_level", "VOLUME_5"},
		{"Change your eye color to purple", "intent_imperative_eyecolor_specific_extend", "eye_color", "COLOR_PURPLE"},
		{"Make your eyes blue.", "intent_imperative_eyecolor_specific_extend", "eye_color", "COLOR_BLUE"},
		{"Turn left", "intent_imperative_turnleft", "", ""},
		{"Stop.", "intent_global_stop_extend", "entity_behavior_stoppable", ""},
	}
	for _, c := range cases {
		got, ok := route(c.text)
		if !ok {
			t.Errorf("%q: not routed, want %s", c.text, c.intent)
			continue
		}
		if got.Intent != c.intent {
			t.Errorf("%q: intent %s, want %s", c.text, got.Intent, c.intent)
		}
		if c.param != "" && got.Params[c.param] != c.value {
			t.Errorf("%q: %s=%q, want %q", c.text, c.param, got.Params[c.param], c.value)
		}
	}
}

// These belong to Home Assistant and must not be taken by Vector's rules.
func TestRouteLeavesHomeAssistantCommands(t *testing.T) {
	for _, text := range []string{
		"Turn on the laundry room lights",
		"Turn left lamp on",
		"What time is it?",
		"What's the weather tomorrow?",
		"Stop the music in the kitchen",
		"Set the thermostat to 70",
		"Is the garage door open?",
		"Lock the front door",
		"Set the living room lights to blue",
		"Turn up the volume on the TV",
		"My name is going to be on the list",
	} {
		if got, ok := route(text); ok {
			t.Errorf("%q routed to %s; should go to Home Assistant", text, got.Intent)
		}
	}
}
