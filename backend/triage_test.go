package main

import "testing"

func TestTriageRed(t *testing.T) {
	tr, err := LoadTriage("../triage_rules.json")
	if err != nil {
		t.Fatal(err)
	}
	hit := []string{
		"У меня сильная БОЛЬ в груди!!!",
		"болит грудь, отдаёт в руку",
		"Онемела половина лица",
		"мне трудно... дышать",
		"Сильное кровотечение из носа",
		"мама потеряла сознание",
		"не хочу жить",
		"кеудем қатты ауырып тұр",
		"бетімнің жартысы ұйып қалды",
		"тыныс ала алмай жатырмын",
		"есінен танып қалды",
		"ес-түссіз жатыр",
		"өмір сүргім келмейді",
	}
	for _, s := range hit {
		if _, _, ok := tr.CheckRed(s); !ok {
			t.Errorf("expected red: %q", s)
		}
	}
	miss := []string{
		"болит горло третий день",
		"хочу к дерматологу, сыпь на руке",
		"тамағым ауырады",
		"сколько стоит УЗИ?",
	}
	for _, s := range miss {
		if p, _, ok := tr.CheckRed(s); ok {
			t.Errorf("false positive %q matched %q", s, p)
		}
	}
}

func TestMaxUrgencyOnlyUp(t *testing.T) {
	if maxUrgency("red", "green") != "red" || maxUrgency("yellow", "green") != "yellow" || maxUrgency("green", "yellow") != "yellow" {
		t.Fatal("urgency must never go down")
	}
}
