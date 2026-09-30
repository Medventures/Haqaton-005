import { useEffect, useRef, useState, type FormEvent } from 'react'
import { ArrowUp, Paperclip, PhoneCall, SquarePen } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { api } from '../api/client'
import type { Dialog, DialogResponse, Language, Message } from '../api/types'
import { ServiceOffers } from '../components/Booking'
import { ATTACHMENT_TYPES, AttachmentList, attachmentsOf, isAllowedAttachment } from '../components/Attachments'

export default function PatientPage() {
  const { t, i18n } = useTranslation()
  const [messages, setMessages] = useState<Message[]>([])
  const [dialog, setDialog] = useState<Dialog | null>(null)
  const [language, setLanguage] = useState<Language>('ru')
  const [input, setInput] = useState('')
  const [typing, setTyping] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [error, setError] = useState('')
  const [ready, setReady] = useState(false)
  const [dismissedRed, setDismissedRed] = useState('')
  // operator-message count at the moment of an urgent handoff; null = not waiting for a specialist
  const [waitingFrom, setWaitingFrom] = useState<number | null>(null)
  const bottom = useRef<HTMLDivElement>(null)
  const fileInput = useRef<HTMLInputElement>(null)
  const busy = typing || uploading
  const selectLanguage = (next: Language) => { setLanguage(next); void i18n.changeLanguage(next) }
  // keyed by content: the local reply has no id, the polled copy of the same message does
  const redKey = (message: Message) => message.content
  const latestRed = [...messages].reverse().find(message => message.role === 'bot' && message.data?.urgency === 'red')
  const showRisk = latestRed && redKey(latestRed) !== dismissedRed
  const operatorCount = messages.filter(message => message.role === 'operator').length
  const waitingSpecialist = waitingFrom !== null && dialog?.status === 'operator' && operatorCount <= waitingFrom

  useEffect(() => {
    let active = true
    async function initialize() {
      try {
        if (!localStorage.getItem('patient_jwt')) {
          const session = await api.patient()
          localStorage.setItem('patient_jwt', session.token)
        }
        const id = localStorage.getItem('clinic_dialog_id')
        if (id) {
          const data = await api.dialog(id)
          if (active) applyDialog(data)
        }
      } catch (e) { if (active) setError(e instanceof Error ? e.message : t('network')) }
      finally { if (active) setReady(true) }
    }
    void initialize()
    return () => { active = false }
  }, [])
  function applyDialog(data: DialogResponse) {
    setDialog(data.dialog); setMessages(data.messages || []); selectLanguage(data.dialog.language || 'ru')
  }
  // scroll only the message list: scrollIntoView would also scroll the window and hide the header on mobile
  useEffect(() => { const list = bottom.current?.parentElement; list?.scrollTo({ top: list.scrollHeight, behavior: 'smooth' }) }, [messages, busy, waitingSpecialist])
  useEffect(() => {
    if (!dialog?.id || dialog.status !== 'operator') return
    const timer = window.setInterval(() => api.dialog(dialog.id).then(applyDialog).catch(() => {}), 3000)
    return () => window.clearInterval(timer)
  }, [dialog?.id, dialog?.status])

  async function sendMessage(rawMessage: string) {
    const message = rawMessage.trim()
    if (!message || busy) return
    setInput(''); setError(''); setMessages(current => [...current, { role: 'patient', content: message }]); setTyping(true)
    try {
      const result = await api.chat(message, dialog?.id)
      localStorage.setItem('clinic_dialog_id', result.dialog_id)
      selectLanguage(result.language)
      setDialog({ id: result.dialog_id, status: result.status, language: result.language, urgency: result.urgency, urgency_reason: result.urgency_reason })
      if (result.reply) {
        const reply = { ...result.reply, data: { ...result.reply.data, urgency: result.reply.data?.urgency || result.urgency, actions: result.reply.data?.actions || result.actions, services: result.reply.data?.services || result.services, doctors: result.reply.data?.doctors || result.doctors } }
        setMessages(current => [...current, reply])
      }
    } catch (e) {
      // The connection may drop while the server still answers (flaky Wi-Fi, tunnel): the backend
      // finishes and saves the reply, so fetch it from the dialog before showing an error.
      if (!(e instanceof TypeError) || !dialog?.id || !(await recoverReply(dialog.id))) setError(e instanceof Error ? e.message : t('network'))
    }
    finally { setTyping(false) }
  }
  async function recoverReply(id: string) {
    const botBefore = messages.filter(m => m.role === 'bot').length
    for (let i = 0; i < 20; i++) {
      await new Promise(r => setTimeout(r, 3000))
      try {
        const data = await api.dialog(id)
        if ((data.messages || []).filter(m => m.role === 'bot').length > botBefore) { applyDialog(data); return true }
      } catch { /* still offline */ }
    }
    return false
  }
  async function attachFile(file: File) {
    if (busy) return
    if (!isAllowedAttachment(file)) { setError(t('attachmentError')); return }
    setError(''); setUploading(true)
    try {
      const result = await api.upload(file, dialog?.id)
      localStorage.setItem('clinic_dialog_id', result.dialog_id)
      setMessages(current => [...current, result.message, result.reply])
      if (dialog) setDialog({ ...dialog, id: result.dialog_id, status: result.status })
      // a first upload opens the dialog: take its urgency/language from the server instead of guessing
      else api.dialog(result.dialog_id).then(data => setDialog({ ...data.dialog, status: result.status })).catch(() => {})
    } catch (e) { setError(e instanceof Error ? e.message : t('network')) }
    finally { setUploading(false) }
  }
  // «Новый чат»: new patient session, the previous dialog stays with the clinic
  async function newChat() {
    if (busy) return
    localStorage.removeItem('clinic_dialog_id')
    setMessages([]); setDialog(null); setError(''); setWaitingFrom(null); setDismissedRed('')
    try { const session = await api.patient(); localStorage.setItem('patient_jwt', session.token) } catch { /* keep the old token */ }
  }
  function send(event: FormEvent) { event.preventDefault(); void sendMessage(input) }
  async function contactOperator(action = 'contact_operator') {
    if (!dialog?.id) return
    try {
      const result = await api.handoff(dialog.id)
      setDialog(current => current ? { ...current, status: 'operator' } : current)
      setMessages(current => [...current, result.reply])
      if (action === 'urgent_operator') {
        setWaitingFrom(operatorCount)
        if (latestRed) setDismissedRed(redKey(latestRed))
      }
    } catch (e) { setError(e instanceof Error ? e.message : t('network')) }
  }

  const riskActions = latestRed?.data?.actions || []
  return <main className="patient-page">
    <header className="site-header patient-site-header"><a className="clinic-brand" href="/"><img src="/logo-mark.svg" alt="" width="28" height="28" />Ana<span>Care</span></a><div className="header-tools">{dialog?.urgency && <span className={`urgency-chip urgency-${dialog.urgency}`} aria-label={t(`urgency.${dialog.urgency}`)}><i />{t(`urgency.${dialog.urgency}`)}</span>}<div className="language-switch" role="group" aria-label="Language"><button className={language === 'ru' ? 'active' : ''} onClick={() => selectLanguage('ru')}>Рус</button><button className={language === 'kk' ? 'active' : ''} onClick={() => selectLanguage('kk')}>Қаз</button><button className={language === 'en' ? 'active' : ''} onClick={() => selectLanguage('en')}>Eng</button></div></div></header>
    {showRisk && latestRed ? <section className="risk-screen"><div className="risk-screen-content"><span className="risk-screen-kicker">{t('riskKicker')}</span><h1>{t('riskTitle')}</h1><p className="risk-screen-lead">{t('riskLead')}</p><p className="risk-screen-message">{latestRed.content}</p><p>{t('riskAdvice')}</p><div className="risk-actions">{riskActions.map(action => action === 'call_103' ? <a className="call-button" key={action} href="tel:103"><PhoneCall size={18}/>{t('actions.call_103')}</a> : (action === 'contact_operator' || action === 'urgent_operator') ? <button className="contact-button" key={action} onClick={() => void contactOperator(action)}>{t(`actions.${action}`)}</button> : null)}</div><p className="medical-disclaimer">{t('disclaimer')}</p><button className="risk-return" onClick={() => setDismissedRed(redKey(latestRed))}>{language === 'ru' ? 'Вернуться в чат' : language === 'kk' ? 'Чатқа оралу' : 'Return to chat'}</button></div></section> : <section className="patient-content"><div className="patient-intro"><span className="eyebrow">ANACARE</span><h1>{t('title')}</h1><p>{t('subtitle')}</p></div>
      <section className="chat-panel" aria-label="Patient chat"><div className="chat-toolbar"><div className="online-indicator"/><span>AnaCare</span><span className="chat-toolbar-sub">· {t('consultation')}</span>{messages.length > 0 && <button type="button" className="new-chat-button" onClick={() => void newChat()} disabled={busy}><SquarePen size={16} strokeWidth={1.75} />{t('newChat')}</button>}</div>
        <div className="chat-messages">{!messages.length && ready && <div className="welcome-content"><div className="welcome-note">{t('greet')}</div><div className="welcome-examples"><span>{t('examples.title')}</span>{(['item1', 'item2', 'item3'] as const).map(key => <button key={key} type="button" disabled={busy} onClick={() => void sendMessage(t(`examples.${key}`))}>{t(`examples.${key}`)}</button>)}</div></div>}
          {messages.map((message, index) => <MessageView key={`${index}-${message.id || message.content.slice(0, 8)}`} message={message} language={language} dialogId={dialog?.id} onOperator={action => void contactOperator(action)} onReply={reply => setMessages(current => [...current, reply])} />)}
          {waitingSpecialist && <div className="chat-message bot-message" role="status"><div className="typing-bubble waiting-bubble"><span className="spinner" aria-hidden="true"/>{t('waitingSpecialist')}</div></div>}
          {busy && <div className="chat-message bot-message"><div className="typing-bubble"><span className="typing-dots"><i/><i/><i/></span>{t('typing')}</div></div>}
          <div ref={bottom}/>
        </div>
        <form className={`message-form${error ? ' has-error' : ''}`} onSubmit={send}><button type="button" className="attach-button" onClick={() => fileInput.current?.click()} disabled={busy || !ready} aria-label={t('attach')} title={t('attach')}><Paperclip size={20} strokeWidth={1.75}/></button><input ref={fileInput} type="file" hidden accept={ATTACHMENT_TYPES.join(',')} onChange={e => { const file = e.target.files?.[0]; e.target.value = ''; if (file) void attachFile(file) }} /><textarea value={input} disabled={uploading} onChange={e => setInput(e.target.value)} placeholder={t('placeholder')} rows={1} maxLength={4000} aria-label={t('placeholder')} onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); void send(e) } }} /><button type="submit" className="send-button" disabled={!input.trim() || busy || !ready} aria-label={t('send')}><ArrowUp size={19}/></button><span className="form-hint">{error || 'Enter ↵'}</span></form>
      </section>
      <footer className="patient-footer">{t('patientFooter')} <a href="tel:103">103</a></footer>
    </section>}
  </main>
}

function MessageView({ message, language, dialogId, onOperator, onReply }: { message: Message; language: Language; dialogId?: string; onOperator: (action: string) => void; onReply: (message: Message) => void }) {
  const { t } = useTranslation()
  const urgency = message.data?.urgency
  const actions = message.data?.actions || []
  const services = message.data?.services || []
  const doctors = message.data?.doctors || []
  const attachments = attachmentsOf(message)
  // the patient's file message carries only the file name as text: show the file, not the name twice
  const showText = !(attachments.length && message.role === 'patient')
  return <div className={`chat-message ${message.role === 'patient' ? 'patient-message' : 'bot-message'}`}>
    {message.role === 'operator' && <span className="operator-label">{t('operatorLabel')}</span>}
    {showText && message.content && <div className="message-bubble">{message.content}</div>}
    {attachments.length > 0 && <AttachmentList attachments={attachments} />}
    {urgency && urgency !== 'green' && <div className="medical-disclaimer">{t('disclaimer')}</div>}
    {actions.includes('call_103') && <a className="call-button" href="tel:103"><PhoneCall size={18}/>{t('actions.call_103')}</a>}
    {actions.filter(action => action === 'contact_operator' || action === 'urgent_operator').map(action => <button className="contact-button" key={action} onClick={() => onOperator(action)}>{t(`actions.${action}`)}</button>)}
    {(services.length > 0 || doctors.length > 0) && <ServiceOffers services={services} doctors={doctors} language={language} dialogId={dialogId} onReply={onReply} />}
  </div>
}
