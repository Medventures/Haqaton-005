import { useEffect, useRef, useState, type FormEvent } from 'react'
import { ArrowLeft, Check, Send } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { api } from '../api/client'
import type { DialogResponse, Ticket, Urgency } from '../api/types'

export default function OperatorPage() {
  const { t, i18n } = useTranslation()
  const level = (urgency: Urgency) => t(`urgency.${urgency}`)
  const [authenticated, setAuthenticated] = useState(Boolean(localStorage.getItem('operator_token')))
  const [username, setUsername] = useState('operator1')
  const [password, setPassword] = useState('operator1')
  const [tickets, setTickets] = useState<Ticket[]>([])
  const [selected, setSelected] = useState('')
  const [conversation, setConversation] = useState<DialogResponse | null>(null)
  const [reply, setReply] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const bottom = useRef<HTMLDivElement>(null)

  async function login(event: FormEvent) {
    event.preventDefault(); setBusy(true); setError('')
    try { const session = await api.operatorLogin(username, password); localStorage.setItem('clinic_token', session.token); localStorage.setItem('operator_token', 'true'); setAuthenticated(true) }
    catch (e) { setError(e instanceof Error ? e.message : t('operator.loginError')) }
    finally { setBusy(false) }
  }
  useEffect(() => {
    if (!authenticated) return
    let live = true
    const refresh = () => api.queue().then(items => { if (live) setTickets(items) }).catch(e => live && setError(e instanceof Error ? e.message : t('operator.errorQueue')))
    void refresh(); const timer = window.setInterval(refresh, 5000)
    return () => { live = false; window.clearInterval(timer) }
  }, [authenticated])
  useEffect(() => { if (!selected && tickets.length) setSelected(tickets[0].dialog_id) }, [selected, tickets])
  useEffect(() => {
    if (!authenticated || !selected) { setConversation(null); return }
    let live = true
    const refresh = () => api.dialog(selected).then(d => { if (live) setConversation(d) }).catch(e => live && setError(e instanceof Error ? e.message : t('operator.errorDialog')))
    void refresh(); const timer = window.setInterval(refresh, 3000)
    return () => { live = false; window.clearInterval(timer) }
  }, [authenticated, selected])
  useEffect(() => { bottom.current?.scrollIntoView({ behavior: 'smooth' }) }, [conversation?.messages.length])
  async function send(event: FormEvent) {
    event.preventDefault(); const text = reply.trim(); if (!text || !selected) return
    setBusy(true)
    try { await api.reply(selected, text); setReply(''); setConversation(await api.dialog(selected)) }
    catch (e) { setError(e instanceof Error ? e.message : t('operator.errorReply')) }
    finally { setBusy(false) }
  }
  async function closeTicket() {
    if (!selected) return
    try { await api.reply(selected, undefined, true); setTickets(current => current.filter(t => t.dialog_id !== selected)); setSelected(''); setConversation(null) }
    catch (e) { setError(e instanceof Error ? e.message : t('operator.errorClose')) }
  }
  if (!authenticated) return <main className="operator-login-page"><a className="clinic-brand" href="/">MedHub <span>Clinic</span></a><form className="login-card" onSubmit={login}><span className="eyebrow">{t('operator.workplace')}</span><h1>{t('operator.loginTitle')}</h1><label>{t('operator.username')}<input value={username} onChange={e => setUsername(e.target.value)} autoComplete="username" required /></label><label>{t('operator.password')}<input type="password" value={password} onChange={e => setPassword(e.target.value)} autoComplete="current-password" required /></label>{error && <p className="form-error">{error}</p>}<button className="primary-button" disabled={busy}>{busy ? t('operator.loggingIn') : t('operator.login')}</button></form></main>
  return <main className="operator-page"><header className="site-header operator-site-header"><a className="clinic-brand" href="/">MedHub <span>Clinic</span></a><span className="operator-heading">{t('operator.heading')}</span><div className="operator-header-actions"><div className="language-switch" role="group" aria-label="Language"><button className={i18n.language === 'ru' ? 'active' : ''} onClick={() => void i18n.changeLanguage('ru')}>Рус</button><button className={i18n.language === 'kk' ? 'active' : ''} onClick={() => void i18n.changeLanguage('kk')}>Қаз</button><button className={i18n.language === 'en' ? 'active' : ''} onClick={() => void i18n.changeLanguage('en')}>Eng</button></div><a className="back-patient" href="/">{t('operator.patientChat')}</a></div></header>{error && <div className="inline-error">{error}<button onClick={() => setError('')}>×</button></div>}<div className="operator-layout"><aside className={`queue-pane ${selected ? 'mobile-hide' : ''}`}><div className="queue-title"><div><h1>{t('operator.queue')}</h1><p>{t('operator.cases')}</p></div><span>{tickets.length}</span></div><div className="ticket-list">{tickets.map(ticket => { const weeks = ticket.pregnancy_weeks ?? ticket.pregnancy_week ?? ticket.gestational_weeks; return <button className={`ticket-row ${selected === ticket.dialog_id ? 'selected' : ''} urgency-border-${ticket.urgency}`} key={ticket.id} onClick={() => setSelected(ticket.dialog_id)}><div className="ticket-meta"><span className={`urgency-text urgency-text-${ticket.urgency}`}>{level(ticket.urgency)}</span><time>{ticket.created_at ? new Date(ticket.created_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : ''}</time></div><strong>{ticket.reason}</strong>{ticket.pregnant && weeks != null && <span className="pregnancy-badge">{t('operator.pregnancy', { weeks })}</span>}<p>{ticket.last_message || ticket.summary}</p></button>})}{!tickets.length && <p className="queue-empty">{t('operator.emptyQueue')}</p>}</div></aside>
    <section className={`conversation-pane ${selected ? 'mobile-show' : ''}`}>{!selected ? <div className="conversation-empty">{t('operator.noSelection')}</div> : <><header className="conversation-header"><button className="mobile-back" onClick={() => setSelected('')}><ArrowLeft size={16}/> {t('operator.back')}</button><div className="conversation-head-copy"><strong>{t('operator.conversation')}</strong><span>#{selected.slice(-8)}</span></div><button className="close-ticket" onClick={closeTicket}><Check size={15}/> {t('operator.close')}</button></header><section className="summary-panel"><span className="eyebrow">{t('operator.summary')}</span><p>{conversation?.dialog.summary || t('operator.loadingSummary')}</p><span className="eyebrow">{t('operator.reason')}</span><p>{conversation?.dialog.urgency_reason || tickets.find(ticket => ticket.dialog_id === selected)?.reason || t('operator.noReason')}</p></section><div className="conversation-messages">{conversation?.messages.map((message, index) => <article className={`conversation-message ${message.role === 'operator' ? 'operator-own' : ''}`} key={`${index}-${message.id || message.content.slice(0, 8)}`}><span>{message.role === 'patient' ? t('operator.patient') : message.role === 'operator' ? t('operator.you') : t('operator.bot')}</span><p>{message.content}</p><time>{message.created_at ? new Date(message.created_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : ''}</time></article>)}<div ref={bottom}/></div><form className="reply-form" onSubmit={send}><textarea value={reply} onChange={e => setReply(e.target.value)} placeholder={t('operator.replyPlaceholder')} rows={2} /><button disabled={!reply.trim() || busy}><Send size={16}/><span>{t('operator.reply')}</span></button></form></>}</section></div></main>
}
