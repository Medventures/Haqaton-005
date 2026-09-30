export type Lang = 'ru' | 'kk' | 'en'
export type Urgency = 'green' | 'yellow' | 'red'

export type DemoStep =
  | { kind: 'patient'; text: string }
  | { kind: 'bot'; text: string; urgency?: Urgency; card?: { name: string; price: string; slots: string[] }; actions?: string[] }

type Dict = {
  nav: { how: string; levels: string; operators: string; api: string; open: string }
  hero: { title: string; lead: string; open: string; operator: string; replay: string; demoLabel: string }
  levels: Record<Urgency, string>
  demo: DemoStep[][]
  typing: string
  disclaimer: string
  how: { title: string; lead: string; steps: { title: string; text: string }[] }
  scale: {
    title: string
    lead: string
    items: Record<Urgency, { name: string; text: string }>
    note: string
  }
  ops: { title: string; lead: string; summaryTitle: string; want: string; found: string; why: string; queue: string; tickets: { text: string; u: Urgency; reason: string }[]; pregnant: string }
  api: { title: string; lead: string; spec: string; rows: [string, string, string][] }
  footer: { safety: string; stack: string }
}

export const dict: Record<Lang, Dict> = {
  ru: {
    nav: { how: 'Как работает', levels: 'Срочность', operators: 'Операторам', api: 'Интеграция', open: 'Открыть чат' },
    hero: {
      title: 'Подскажет, к какому врачу идти. И заметит, когда ждать нельзя.',
      lead: 'Чат клиники на русском, казахском и английском. Подбирает специалиста, услуги с ценами и свободное время. Опасные симптомы распознаёт до ИИ и сразу направляет к 103.',
      open: 'Открыть чат',
      operator: 'Вход для операторов',
      replay: 'Показать снова',
      demoLabel: 'Пример диалога',
    },
    levels: { green: 'Плановое', yellow: 'В ближайшее время', red: 'Срочно' },
    demo: [
      [
        { kind: 'patient', text: 'Третий день болит горло и заложен нос' },
        { kind: 'bot', text: 'Вам подойдёт ЛОР-врач. Ближайшее время — завтра в 10:00.', urgency: 'green', card: { name: 'Консультация ЛОР-врача', price: '9 500 ₸', slots: ['чт 10:00', 'пт 17:00'] } },
      ],
      [
        { kind: 'patient', text: 'Второй день тянет низ живота' },
        { kind: 'bot', text: 'Есть ли у вас беременность или её вероятность?', urgency: 'green' },
        { kind: 'patient', text: 'Да, я на 32 неделе' },
        { kind: 'bot', text: 'Ваши симптомы могут требовать срочной помощи. Срочно звоните 103.', urgency: 'red', actions: ['Вызвать скорую — 103', 'Срочно связаться с оператором'] },
      ],
    ],
    typing: 'печатает',
    disclaimer: 'Оценка ориентировочная, не является диагнозом',
    how: {
      title: 'Что происходит с каждым сообщением',
      lead: 'Порядок один и тот же для каждого сообщения пациента. ИИ подключается только там, где без него не обойтись.',
      steps: [
        { title: 'Проверка опасных симптомов', text: 'Сообщение сверяется со списком фраз на трёх языках — без ИИ. Боль в груди, онемение лица, у беременной — подтекание вод. Если совпало, пациент сразу видит кнопку 103, а диалог уходит оператору.' },
        { title: 'Разбор жалобы', text: 'Локальная модель определяет язык, специальность из каталога клиники и срочность. Ответ строго по схеме: придумать специальность, которой нет в каталоге, она не может.' },
        { title: 'Уточнение', text: 'Если жалоба слишком общая, бот задаёт не больше двух вопросов. При боли внизу живота — один раз спрашивает о беременности.' },
        { title: 'Подбор', text: 'Услуги, цены и врачи со свободными слотами берутся из каталога клиники кодом, а не генерируются.' },
        { title: 'Ответ или оператор', text: 'Бот отвечает только по данным клиники и не ставит диагнозов. Если помочь не вышло — диалог уходит оператору с кратким описанием.' },
      ],
    },
    scale: {
      title: 'Три уровня срочности',
      lead: 'Уровень виден пациенту в шапке чата и оператору в очереди. В течение диалога он может только расти.',
      items: {
        green: { name: 'Плановое', text: 'Обычный подбор врача и услуги.' },
        yellow: { name: 'В ближайшее время', text: 'Бот советует не откладывать и предлагает связаться с оператором. Подбор услуги остаётся.' },
        red: { name: 'Срочно', text: 'Подбор прекращается. Кнопка «Вызвать скорую — 103», диалог — первым в очереди оператора.' },
      },
      note: 'Красный уровень определяется кодом по списку фраз, а не моделью. Список лежит в triage_rules.json и редактируется без перезапуска кода.',
    },
    ops: {
      title: 'Операторам: срочное всегда сверху',
      lead: 'Очередь сортируется по срочности. Открыв обращение, оператор сразу видит, что хотел пациент и почему бот передал диалог.',
      summaryTitle: 'Обращение',
      want: 'Что хотел пациент',
      found: 'Что выяснено',
      why: 'Почему передано',
      queue: 'Очередь',
      pregnant: 'Беременность, 32 нед.',
      tickets: [
        { text: 'Хочу поговорить с администратором', u: 'green', reason: 'Попросил оператора' },
        { text: 'Температура 39 второй день', u: 'yellow', reason: 'Кнопка «Связаться с оператором»' },
        { text: 'Не могу понять, к кому записаться', u: 'green', reason: 'Не удалось подобрать специальность' },
        { text: 'Да, я на 32 неделе', u: 'red', reason: 'Красный флаг: низ живота' },
      ],
    },
    api: {
      title: 'Подключается к сайту и МИС',
      lead: 'Весь функционал доступен через REST API с OpenAPI-описанием. Модель работает локально в клинике — данные пациентов не уходят во внешние сервисы.',
      spec: 'Описание API',
      rows: [
        ['POST', '/api/chat', 'Сообщение пациента → ответ, срочность, услуги'],
        ['GET', '/api/operator/queue', 'Очередь обращений по срочности'],
        ['GET', '/api/dialogs/:id', 'Диалог целиком'],
        ['POST', '/api/operator/reply', 'Ответ оператора пациенту'],
      ],
    },
    footer: {
      safety: 'Бот не ставит диагноз и не назначает лечение. Оценка срочности ориентировочная. При угрозе жизни звоните 103.',
      stack: 'Сделано на Go, Postgres и React. Языковая модель работает локально в LM Studio.',
    },
  },
  kk: {
    nav: { how: 'Қалай жұмыс істейді', levels: 'Шұғылдық', operators: 'Операторларға', api: 'Интеграция', open: 'Чатты ашу' },
    hero: {
      title: 'Қай дәрігерге бару керегін айтады. Күтуге болмайтынын да байқайды.',
      lead: 'Клиниканың қазақ, орыс және ағылшын тіліндегі чаты. Маманды, бағасымен қызметтерді және бос уақытты табады. Қауіпті белгілерді ЖИ-ге дейін анықтап, бірден 103-ке бағыттайды.',
      open: 'Чатты ашу',
      operator: 'Операторларға кіру',
      replay: 'Қайта көрсету',
      demoLabel: 'Диалог үлгісі',
    },
    levels: { green: 'Жоспарлы', yellow: 'Жақын арада', red: 'Шұғыл' },
    demo: [
      [
        { kind: 'patient', text: 'Үш күннен бері тамағым ауырады, мұрным бітеліп тұр' },
        { kind: 'bot', text: 'Сізге ЛОР дәрігер керек. Ең жақын уақыт — ертең 10:00.', urgency: 'green', card: { name: 'ЛОР дәрігер кеңесі', price: '9 500 ₸', slots: ['бс 10:00', 'жм 17:00'] } },
      ],
      [
        { kind: 'patient', text: 'Екі күннен бері іштің төменгі жағы тартып ауырады' },
        { kind: 'bot', text: 'Сізде жүктілік бар ма немесе болуы мүмкін бе?', urgency: 'green' },
        { kind: 'patient', text: 'Иә, 32 аптадамын' },
        { kind: 'bot', text: 'Сіздегі белгілер шұғыл көмекті қажет етуі мүмкін. Дереу 103-ке қоңырау шалыңыз.', urgency: 'red', actions: ['Жедел жәрдем шақыру — 103', 'Операторға шұғыл хабарласу'] },
      ],
    ],
    typing: 'жазып жатыр',
    disclaimer: 'Бағалау шамамен берілген, диагноз емес',
    how: {
      title: 'Әр хабарламамен не болады',
      lead: 'Пациенттің әр хабарламасы бір ретпен өңделеді. ЖИ онсыз болмайтын жерде ғана қосылады.',
      steps: [
        { title: 'Қауіпті белгілерді тексеру', text: 'Хабарлама үш тілдегі тіркестер тізімімен салыстырылады — ЖИ-сіз. Кеуде ауыруы, беттің ұюы, жүкті әйелде — судың кетуі. Сәйкес келсе, пациент бірден 103 батырмасын көреді, диалог операторға кетеді.' },
        { title: 'Шағымды талдау', text: 'Жергілікті модель тілді, клиника каталогындағы маманды және шұғылдықты анықтайды. Жауап қатаң схема бойынша: каталогта жоқ маманды ойдан шығара алмайды.' },
        { title: 'Нақтылау', text: 'Шағым тым жалпы болса, бот ең көбі екі сұрақ қояды. Іштің төменгі жағы ауырса — жүктілік туралы бір рет сұрайды.' },
        { title: 'Таңдау', text: 'Қызметтер, бағалар және бос уақыты бар дәрігерлер клиника каталогынан кодпен алынады, ойдан шығарылмайды.' },
        { title: 'Жауап немесе оператор', text: 'Бот тек клиника деректері бойынша жауап береді, диагноз қоймайды. Көмектесе алмаса — диалог қысқа сипаттамамен операторға беріледі.' },
      ],
    },
    scale: {
      title: 'Шұғылдықтың үш деңгейі',
      lead: 'Деңгейді пациент чаттың жоғарғы жағынан, оператор кезектен көреді. Диалог барысында ол тек жоғарылайды.',
      items: {
        green: { name: 'Жоспарлы', text: 'Дәрігер мен қызметті әдеттегідей таңдау.' },
        yellow: { name: 'Жақын арада', text: 'Бот кешіктірмеуге кеңес беріп, операторға хабарласуды ұсынады. Қызмет таңдауы сақталады.' },
        red: { name: 'Шұғыл', text: 'Таңдау тоқтайды. «Жедел жәрдем шақыру — 103» батырмасы, диалог оператор кезегінде бірінші.' },
      },
      note: 'Қызыл деңгейді модель емес, код тіркестер тізімі бойынша анықтайды. Тізім triage_rules.json файлында, кодты қайта жазбай өзгертуге болады.',
    },
    ops: {
      title: 'Операторларға: шұғыл өтініш әрқашан жоғарыда',
      lead: 'Кезек шұғылдық бойынша сұрыпталады. Өтінішті ашқан оператор пациенттің не қалағанын және боттың неге бергенін бірден көреді.',
      summaryTitle: 'Өтініш',
      want: 'Пациент не қалады',
      found: 'Не анықталды',
      why: 'Неге берілді',
      queue: 'Кезек',
      pregnant: 'Жүктілік, 32 апта',
      tickets: [
        { text: 'Әкімшімен сөйлескім келеді', u: 'green', reason: 'Операторды сұрады' },
        { text: 'Екі күннен бері қызу 39', u: 'yellow', reason: '«Операторға хабарласу» батырмасы' },
        { text: 'Кімге жазылу керегін түсінбеймін', u: 'green', reason: 'Маман анықталмады' },
        { text: 'Иә, 32 аптадамын', u: 'red', reason: 'Қызыл белгі: іштің төменгі жағы' },
      ],
    },
    api: {
      title: 'Сайтқа және медициналық жүйеге қосылады',
      lead: 'Барлық мүмкіндік OpenAPI сипаттамасы бар REST API арқылы қолжетімді. Модель клиниканың ішінде жұмыс істейді — пациент деректері сыртқы сервистерге кетпейді.',
      spec: 'API сипаттамасы',
      rows: [
        ['POST', '/api/chat', 'Пациент хабарламасы → жауап, шұғылдық, қызметтер'],
        ['GET', '/api/operator/queue', 'Шұғылдық бойынша өтініштер кезегі'],
        ['GET', '/api/dialogs/:id', 'Толық диалог'],
        ['POST', '/api/operator/reply', 'Оператордың пациентке жауабы'],
      ],
    },
    footer: {
      safety: 'Бот диагноз қоймайды және ем тағайындамайды. Шұғылдық бағасы шамамен берілген. Өмірге қауіп төнсе, 103-ке қоңырау шалыңыз.',
      stack: 'Go, Postgres және React негізінде. Тілдік модель LM Studio-да жергілікті жұмыс істейді.',
    },
  },
  en: {
    nav: { how: 'How it works', levels: 'Urgency', operators: 'For operators', api: 'Integration', open: 'Open chat' },
    hero: {
      title: 'Tells patients which doctor to see. And notices when it can’t wait.',
      lead: 'A clinic chat in Kazakh, Russian and English. It finds the right specialist, services with prices and free time slots. Dangerous symptoms are caught before the AI runs, and the patient is sent straight to 103.',
      open: 'Open chat',
      operator: 'Operator sign-in',
      replay: 'Play again',
      demoLabel: 'Sample conversation',
    },
    levels: { green: 'Routine', yellow: 'Soon', red: 'Urgent' },
    demo: [
      [
        { kind: 'patient', text: 'My throat has hurt for three days and my nose is blocked' },
        { kind: 'bot', text: 'An ENT doctor is the right choice. The earliest slot is tomorrow at 10:00.', urgency: 'green', card: { name: 'ENT consultation', price: '9 500 ₸', slots: ['Thu 10:00', 'Fri 17:00'] } },
      ],
      [
        { kind: 'patient', text: 'Pulling pain in my lower abdomen for two days' },
        { kind: 'bot', text: 'Are you pregnant, or could you be pregnant?', urgency: 'green' },
        { kind: 'patient', text: 'Yes, I’m 32 weeks pregnant' },
        { kind: 'bot', text: 'Your symptoms may need urgent medical help. Call 103 now.', urgency: 'red', actions: ['Call an ambulance — 103', 'Contact an operator urgently'] },
      ],
    ],
    typing: 'typing',
    disclaimer: 'This is an estimate, not a diagnosis',
    how: {
      title: 'What happens to every message',
      lead: 'Every patient message goes through the same steps. The AI is used only where nothing else will do.',
      steps: [
        { title: 'Danger check', text: 'The message is matched against a phrase list in three languages — no AI involved. Chest pain, a numb face, leaking fluid in pregnancy. On a match the patient gets a 103 button and the chat goes to an operator.' },
        { title: 'Understanding the complaint', text: 'A local model detects the language, the specialty from the clinic catalog and the urgency. The answer follows a strict schema, so it cannot invent a specialty the clinic doesn’t have.' },
        { title: 'Clarifying', text: 'If the complaint is too vague, the bot asks at most two questions. With lower abdominal pain it asks about pregnancy once.' },
        { title: 'Matching', text: 'Services, prices and doctors with free slots are taken from the clinic catalog by code, not generated.' },
        { title: 'Answer or operator', text: 'The bot answers only from clinic data and never diagnoses. If it can’t help, the chat goes to an operator with a short summary.' },
      ],
    },
    scale: {
      title: 'Three urgency levels',
      lead: 'Patients see the level at the top of the chat, operators see it in the queue. During a conversation it can only go up.',
      items: {
        green: { name: 'Routine', text: 'Regular doctor and service matching.' },
        yellow: { name: 'Soon', text: 'The bot advises not to delay and offers an operator. Service matching stays available.' },
        red: { name: 'Urgent', text: 'Matching stops. A “Call an ambulance — 103” button, and the chat is first in the operator queue.' },
      },
      note: 'Red is decided by code from a phrase list, not by the model. The list lives in triage_rules.json and can be edited without touching the code.',
    },
    ops: {
      title: 'For operators: urgent cases on top',
      lead: 'The queue is sorted by urgency. Opening a case, the operator sees what the patient wanted and why the bot handed it over.',
      summaryTitle: 'Case',
      want: 'What the patient wanted',
      found: 'What we found out',
      why: 'Why it was handed over',
      queue: 'Queue',
      pregnant: 'Pregnant, 32 wks',
      tickets: [
        { text: 'I’d like to talk to the front desk', u: 'green', reason: 'Asked for an operator' },
        { text: 'Fever of 39 for two days', u: 'yellow', reason: '“Contact an operator” button' },
        { text: 'Not sure who to book with', u: 'green', reason: 'Specialty not found' },
        { text: 'Yes, I’m 32 weeks pregnant', u: 'red', reason: 'Red flag: lower abdomen' },
      ],
    },
    api: {
      title: 'Connects to your website and clinic system',
      lead: 'Everything is available through a REST API with an OpenAPI spec. The model runs locally at the clinic — patient data never leaves for external services.',
      spec: 'API reference',
      rows: [
        ['POST', '/api/chat', 'Patient message → reply, urgency, services'],
        ['GET', '/api/operator/queue', 'Cases sorted by urgency'],
        ['GET', '/api/dialogs/:id', 'Full conversation'],
        ['POST', '/api/operator/reply', 'Operator reply to the patient'],
      ],
    },
    footer: {
      safety: 'The bot does not diagnose or prescribe treatment. The urgency level is an estimate. If a life is at risk, call 103.',
      stack: 'Built with Go, Postgres and React. The language model runs locally in LM Studio.',
    },
  },
}
