package main

import "testing"

// Routing of real questions against the real knowledge.json.
func TestKnowledgeRouting(t *testing.T) {
	k, err := LoadKnowledge("../knowledge.json")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"Где вы находитесь?":                       "address",
		"Қай жерде орналасқансыздар?":              "address",
		"Во сколько открываетесь в субботу?":       "hours",
		"Как подготовиться к УЗИ брюшной полости?": "prep_gastro_us",
		"Қан тапсыруға қалай дайындалу керек?":     "prep_cbc",
		"Можно ли оплатить картой?":                "payment",
		"Принимаете ОСМС?":                         "osms",
		"Is there parking?":                        "parking",
		"Как отменить запись?":                     "reschedule_cancel",
		"Как записаться к врачу?":                  "booking",
		"Когда будут готовы результаты анализов?":  "results",
		"Можно с ребенком прийти?":                 "children",
		"Болит горло третий день":                  "",
		"Хочу к кардиологу":                        "",
		"Сколько стоит консультация ЛОРа?":         "",
	}
	for q, want := range cases {
		e, _ := k.Search(q)
		got := ""
		if e != nil {
			got = e.ID
		}
		if got != want {
			t.Errorf("%q -> %q, want %q", q, got, want)
		}
	}
	for _, e := range k.Entries {
		for _, lang := range []string{"ru", "kk", "en"} {
			if e.Answer[lang] == "" {
				t.Errorf("%s: no %s answer", e.ID, lang)
			}
		}
	}
}
