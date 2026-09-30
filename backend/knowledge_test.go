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
		// pregnancy entries
		"Аяғым қақсап жатыр, жүктімін":                                    "preg_legs",
		"Отекают ноги на 30 неделе беременности":                          "preg_legs",
		"Гастрит беременность кезінде қауіпті ме?":                        "preg_gastritis",
		"Опасен ли гастрит при беременности?":                             "preg_gastritis",
		"Баланың қимылы азайып кетті":                                     "preg_movements",
		"Ребенок мало шевелится":                                          "preg_movements",
		"Можно ли пить таблетки при беременности?":                        "preg_medicines",
		"Токсикоз, тошнит по утрам":                                       "preg_nausea",
		"Болит поясница, я беременна":                                     "preg_back",
		"Жүктілік кезінде басым ауырады":                                  "preg_headache",
		"Какие выделения при беременности нормальные?":                    "preg_discharge",
		"Простуда при беременности, что делать?":                          "preg_cold",
		"Какие витамины пить при беременности?":                           "preg_nutrition",
		"Когда делают скрининг при беременности?":                         "preg_screenings",
		"When should I go to the doctor urgently when pregnant? Bleeding": "preg_urgent",
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
	// general complaints (no pregnancy context) must stay below the 2-hit threshold used for complaints
	for _, q := range []string{
		"Температура и кашель третий день",
		"Тошнота и рвота после еды",
		"Болит голова и давление",
		"Болят ноги к вечеру",
		"Гастрит и изжога",
		"Можно ли пить эти таблетки?",
		"Аяғым ауырады",
	} {
		if e, n := k.Search(q); n >= 2 {
			t.Errorf("%q -> %q with %d hits, want < 2", q, e.ID, n)
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
