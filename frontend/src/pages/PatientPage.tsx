import { useEffect, useRef, useState, type FormEvent } from 'react'
import { ArrowUp, PhoneCall } from 'lucide-react'
import { api } from '../api/client'
import type { Dialog, DialogResponse, Language, Message, Urgency } from '../api/types'

const labels: Record<Language, Record<Urgency, string>> = {
  ru: { green: 'Плановое обращение', yellow: 'Обратитесь в ближайшее время', red: 'Срочно' },
  kk: { green: 'Жоспарлы өтініш', yellow: 'Жақын уақытта көмекке жүгініңіз', red: 'Шұғыл' },
}
const copy = {
  ru: { title: 'Чем можем помочь?', subtitle: 'Опишите, что вас беспокоит, и мы подскажем следующий шаг.', placeholder: 'Напишите о своих симптомах…', send: 'Отправить', typing: 'Подбираем специалиста…', book: 'Записаться', operator: 'Связаться с оператором', emergency: 'Вызвать скорую помощь — 103', disclaimer: 'Оценка ориентировочная, не является диагнозом', network: 'Не удалось отправить сообщение. Проверьте соединение.', greet: 'Здравствуйте. Опишите, пожалуйста, что вас беспокоит.', doctors: 'Врачи и свободное время', accepted: 'Заявка принята' },
  kk: { title: 'Қалай көмектесе аламыз?', subtitle: 'Мазалаған белгілеріңізді жазыңыз, келесі қадамды бірге анықтаймыз.', placeholder: 'Белгілеріңізді жазыңыз…', send: 'Жіберу', typing: 'Маман іздеп жатырмыз…', book: 'Жазылу', operator: 'Оператормен байланысу', emergency: 'Жедел жәрдем шақыру — 103', disclaimer: 'Бағалау бағдарлы, диагноз болып саналмайды', network: 'Хабарлама жіберілмеді. Байланысты тексеріңіз.', greet: 'Сәлеметсіз бе. Сізді не мазалайтынын сипаттап беріңіз.', doctors: 'Дәрігерлер және бос уақыт', accepted: 'Өтінім қабылданды' },
}

export default function PatientPage() {
  const [messages, setMessages] = useState<Message[]>([])
  const [dialog, setDialog] = useState<Dialog | null>(null)
  const [language, setLanguage] = useState<Language>('ru')
  const [input, setInput] = useState('')
  const [typing, setTyping] = useState(false)
  const [error, setError] = useState('')
  const [toast, setToast] = useState('')
  const [ready, setReady] = useState(false)
  const bottom = useRef<HTMLDivElement>(null)
  const t = copy[language]

  useEffect(() => {
    let active = true
    async function initialize() {
      try {
        if (!localStorage.getItem('clinic_token')) {
          const session = await api.patient()
          localStorage.setItem('clinic_token', session.token)
        }
        const id = localStorage.getItem('clinic_dialog_id')
        if (id) {
          const data = await api.dialog(id)
          if (active) applyDialog(data)
        }
      } catch (e) { if (active) setError(e instanceof Error ? e.message : t.network) }
      finally { if (active) setReady(true) }
    }
    void initialize()
    return () => { active = false }
  }, [])
  function applyDialog(data: DialogResponse) {
    setDialog(data.dialog); setMessages(data.messages || []); setLanguage(data.dialog.language || 'ru')
  }
  useEffect(() => { bottom.current?.scrollIntoView({ behavior: 'smooth' }) }, [messages, typing])
  useEffect(() => {
    if (!dialog?.id || dialog.status !== 'operator') return
    const timer = window.setInterval(() => api.dialog(dialog.id).then(applyDialog).catch(() => {}), 3000)
    return () => window.clearInterval(timer)
  }, [dialog?.id, dialog?.status])

  async function send(event: FormEvent) {
    event.preventDefault()
    const message = input.trim()
    if (!message || typing) return
    setInput(''); setError(''); setMessages(current => [...current, { role: 'patient', content: message }]); setTyping(true)
    try {
      const result = await api.chat(message, dialog?.id)
      localStorage.setItem('clinic_dialog_id', result.dialog_id)
      setLanguage(result.language)
      setDialog({ id: result.dialog_id, status: result.status, language: result.language, urgency: result.urgency, urgency_reason: result.urgency_reason })
      if (result.reply) {
        const reply = { ...result.reply, data: { ...result.reply.data, urgency: result.reply.data?.urgency || result.urgency, actions: result.reply.data?.actions || result.actions, services: result.reply.data?.services || result.services, doctors: result.reply.data?.doctors || result.doctors } }
        setMessages(current => [...current, reply])
      }
    } catch (e) { setError(e instanceof Error ? e.message : t.network) }
    finally { setTyping(false) }
  }
  async function contactOperator() {
    if (!dialog?.id) return
    try {
      const result = await api.handoff(dialog.id)
      setDialog(current => current ? { ...current, status: 'operator' } : current)
      setMessages(current => [...current, result.reply])
    } catch (e) { setError(e instanceof Error ? e.message : t.network) }
  }
  function showToast() { setToast(t.accepted); window.setTimeout(() => setToast(''), 2400) }

  return <main className="patient-page">
    <header className="site-header"><a className="clinic-brand" href="/">MedHub <span>Clinic</span></a><div className="header-tools">{dialog && <span className={`urgency-chip urgency-${dialog.urgency}`}><i />{labels[language][dialog.urgency]}</span>}<button className="language-toggle" onClick={() => setLanguage(language === 'ru' ? 'kk' : 'ru')} aria-label="Change language">{language.toUpperCase()}</button></div></header>
    <section className="patient-content"><div className="patient-intro"><span className="eyebrow">MEDHUB CLINIC</span><h1>{t.title}</h1><p>{t.subtitle}</p></div>
      <section className="chat-panel" aria-label="Patient chat"><div className="chat-toolbar"><div className="online-indicator"/><span>MedHub Clinic</span><span className="chat-toolbar-sub">· {language === 'ru' ? 'Консультация' : 'Кеңес'}</span></div>
        <div className="chat-messages">{!messages.length && ready && <div className="welcome-note">{t.greet}</div>}
          {messages.map((message, index) => <MessageView key={`${index}-${message.id || message.content.slice(0, 8)}`} message={message} language={language} onOperator={contactOperator} onBook={showToast} />)}
          {typing && <div className="chat-message bot-message"><div className="typing-bubble"><span className="typing-dots"><i/><i/><i/></span>{t.typing}</div></div>}
          <div ref={bottom}/>
        </div>
        <form className="message-form" onSubmit={send}><textarea value={input} onChange={e => setInput(e.target.value)} placeholder={t.placeholder} rows={1} maxLength={4000} aria-label={t.placeholder} onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); void send(e) } }} /><button type="submit" disabled={!input.trim() || typing || !ready} aria-label={t.send}><ArrowUp size={19}/></button><span className="form-hint">{error || 'Enter ↵'}</span></form>
      </section>
      <footer className="patient-footer">{language === 'ru' ? 'Чат не заменяет консультацию врача.' : 'Чат дәрігер кеңесін алмастырмайды.'} <a href="tel:103">103</a></footer>
    </section>{toast && <div className="toast" role="status">{toast}</div>}
  </main>
}

function MessageView({ message, language, onOperator, onBook }: { message: Message; language: Language; onOperator: () => void; onBook: () => void }) {
  const t = copy[language]
  const urgency = message.data?.urgency
  const call = message.data?.actions?.includes('call_103')
  const contact = message.data?.actions?.includes('contact_operator')
  const services = message.data?.services || []
  const doctors = message.data?.doctors || []
  return <div className={`chat-message ${message.role === 'patient' ? 'patient-message' : 'bot-message'}`}>
    {message.role === 'operator' && <span className="operator-label">Оператор</span>}
    <div className="message-bubble">{message.content}</div>
    {urgency && urgency !== 'green' && <div className="medical-disclaimer">{t.disclaimer}</div>}
    {call && <a className="call-button" href="tel:103"><PhoneCall size={18}/>{t.emergency}</a>}
    {contact && <button className="contact-button" onClick={onOperator}>{t.operator}</button>}
    {services.length > 0 && <div className="services-list">{services.map(service => <article className="service-card" key={service.id}><h3>{service.name}</h3><p>{service.description}</p><div className="service-bottom"><strong>{service.price.toLocaleString(language === 'kk' ? 'kk-KZ' : 'ru-RU')} ₸</strong><button onClick={onBook}>{t.book}</button></div></article>)}</div>}
    {doctors.length > 0 && <section className="doctors-block"><h4>{t.doctors}</h4>{doctors.map(doctor => <div className="doctor-row" key={doctor.id}><b>{doctor.name}</b><div className="slots">{doctor.slots.map(slot => <span key={slot}>{formatSlot(slot, language)}</span>)}</div></div>)}</section>}
  </div>
}
function formatSlot(value: string, language: Language) {
  return new Intl.DateTimeFormat(language === 'kk' ? 'kk-KZ' : 'ru-RU', { weekday: 'short', day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' }).format(new Date(value))
}
