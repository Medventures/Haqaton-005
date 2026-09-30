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
		if _, _, ok := tr.CheckRed(s, false); !ok {
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
		if p, _, ok := tr.CheckRed(s, false); ok {
			t.Errorf("false positive %q matched %q", s, p)
		}
	}
}

func TestMaxUrgencyOnlyUp(t *testing.T) {
	if maxUrgency("red", "green") != "red" || maxUrgency("yellow", "green") != "yellow" || maxUrgency("green", "yellow") != "yellow" {
		t.Fatal("urgency must never go down")
	}
}

func TestDetectPregnancy(t *testing.T) {
	yes := map[string]int{ // text -> weeks (0 = unknown)
		"Я беременна":                      0,
		"я на 32 неделе":                   32,
		"срок 12 недель":                   12,
		"20 неделя беременности":           20,
		"я в положении":                    0,
		"Мен жүктімін":                     0,
		"32-аптадамын":                     32,
		"жүктілік мерзімі 28 апта":         28,
		"екіқабатпын, 16 апталық":          16,
		"у меня 30-я неделя, срок большой": 30,
	}
	for s, w := range yes {
		ok, weeks := DetectPregnancy(s)
		if !ok {
			t.Errorf("expected pregnancy: %q", s)
			continue
		}
		if w == 0 && weeks != nil || w != 0 && (weeks == nil || *weeks != w) {
			t.Errorf("%q: weeks = %v, want %d", s, weeks, w)
		}
	}
	no := []string{
		"екі апта бойы тамағым ауырады",
		"болит горло уже неделю",
		"через 2 недели хочу записаться",
		"задержка 5 дней",
		"2 аптада бір рет басым ауырады",
		"уезжаю на 2 недели",
		"я не беременна",
		"тянет низ живота, беременности нет",
		"мен жүкті емеспін",
	}
	for _, s := range no {
		if ok, _ := DetectPregnancy(s); ok {
			t.Errorf("false pregnancy: %q", s)
		}
	}
}

func TestRedIfPregnantOnlyWhenPregnant(t *testing.T) {
	tr, err := LoadTriage("../triage_rules.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"тянет низ живота", "кровянистые выделения", "отошли воды", "ребенок не шевелится с утра", "іштің төменгі жағы ауырады", "су кетті"} {
		if _, _, ok := tr.CheckRed(s, false); ok {
			t.Errorf("%q must not be red without pregnancy", s)
		}
		if _, _, ok := tr.CheckRed(s, true); !ok {
			t.Errorf("%q must be red when pregnant", s)
		}
	}
	if _, _, ok := tr.CheckRed("боль в груди", false); !ok {
		t.Error("plain red must still work")
	}
}
