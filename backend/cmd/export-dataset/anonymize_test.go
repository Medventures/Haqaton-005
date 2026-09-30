package main

import (
	"reflect"
	"testing"
)

func TestAnonymize(t *testing.T) {
	cases := []struct{ name, in, want string }{
		// phones
		{"phone +7 spaces", "Мой номер +7 701 123 45 67", "Мой номер [ТЕЛЕФОН]"},
		{"phone 8 brackets", "звоните 8 (701) 123-45-67, спасибо", "звоните [ТЕЛЕФОН], спасибо"},
		{"phone contiguous kk", "Нөмірім 87011234567", "Нөмірім [ТЕЛЕФОН]"},
		{"phone +7 dashes kk", "маған +7-777-555-44-33 хабарласыңыз", "маған [ТЕЛЕФОН] хабарласыңыз"},
		// IIN
		{"iin ru", "ИИН 900312300123", "ИИН [ИИН]"},
		{"iin kk", "ЖСН: 850101400567 болады", "ЖСН: [ИИН] болады"},
		{"iin starting with 8", "мой ИИН 870101300456", "мой ИИН [ИИН]"},
		// email
		{"email ru", "пишите на ivan.petrov+med@mail.kz", "пишите на [EMAIL]"},
		{"email kk", "поштам aigul_92@gmail.com", "поштам [EMAIL]"},
		// dates
		{"dob ru", "дата рождения 12.03.1990", "дата рождения [ДАТА]"},
		{"dob kk", "туған күнім 5.11.2001 ж.", "туған күнім [ДАТА] ж."},
		// names
		{"name ru", "Здравствуйте, меня зовут Анна Иванова, болит голова", "Здравствуйте, меня зовут [ИМЯ], болит голова"},
		{"name ru one word", "Меня зовут Олег.", "Меня зовут [ИМЯ]."},
		{"name moe imya", "Мое имя Дмитрий", "Мое имя [ИМЯ]"},
		{"name moyo imya", "моё имя: Светлана", "моё имя: [ИМЯ]"},
		{"name kk menin atym", "Сәлеметсіз бе, менің атым Айгүл Серікқызы", "Сәлеметсіз бе, менің атым [ИМЯ]"},
		{"name kk atym", "Атым Ерлан, басым ауырады", "Атым [ИМЯ], басым ауырады"},
		{"name en", "Hi, my name is John Smith", "Hi, my name is [ИМЯ]"},
		// cards
		{"card grouped", "карта 4400 4301 2345 6789", "карта [КАРТА]"},
		{"card contiguous kk", "картам 4400430123456789", "картам [КАРТА]"},
		{"card dashes", "4400-4301-2345-6789 оплатил", "[КАРТА] оплатил"},
		// combined
		{"combined", "Меня зовут Анна, тел 87011234567, ИИН 900312300123", "Меня зовут [ИМЯ], тел [ТЕЛЕФОН], ИИН [ИИН]"},
		// negatives
		{"temperature", "температура 38", "температура 38"},
		{"temperature decimal", "температура 38.5 уже два дня", "температура 38.5 уже два дня"},
		{"weeks", "я на 32 неделе беременности", "я на 32 неделе беременности"},
		{"price", "прием стоит 9500 тенге", "прием стоит 9500 тенге"},
		{"price spaced", "цена 12 500 ₸", "цена 12 500 ₸"},
		{"time", "можно на 10:00 или 14:30?", "можно на 10:00 или 14:30?"},
		{"kk weeks", "жүктілігім 20 апта", "жүктілігім 20 апта"},
		{"lowercase after intro", "меня зовут не помню как", "меня зовут не помню как"},
		{"atym inside word", "Қатым Ерлан", "Қатым Ерлан"},
		{"short number", "кабинет 8, этаж 2", "кабинет 8, этаж 2"},
		{"i am not masked", "I am Alex", "I am Alex"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Anonymize(c.in); got != c.want {
				t.Errorf("Anonymize(%q)\n got  %q\n want %q", c.in, got, c.want)
			}
		})
	}
}

func TestBuildMessages(t *testing.T) {
	in := []rawMsg{
		{"bot", "Здравствуйте!"},
		{"patient", "болит зуб"},
		{"patient", "мой номер 87011234567"},
		{"bot", "Вам к стоматологу"},
		{"operator", "Запишу вас к стоматологу-терапевту"},
		{"bot", "авто-ответ"},
		{"patient", "спасибо"},
		{"bot", "Пожалуйста"},
		{"patient", "ещё вопрос"},
	}
	want := []chatMsg{
		{"user", "болит зуб\nмой номер [ТЕЛЕФОН]"},
		{"assistant", "Запишу вас к стоматологу-терапевту"},
		{"user", "спасибо"},
		{"assistant", "Пожалуйста"},
	}
	if got := buildMessages(in); !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v\nwant %#v", got, want)
	}
	if got := buildMessages([]rawMsg{{"patient", "алло"}}); len(got) != 0 {
		t.Errorf("unanswered dialog should be empty, got %#v", got)
	}
}
