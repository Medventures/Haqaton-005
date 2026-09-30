import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, ApiError } from '../api/client'
import type { Appointment, Catalog, Language } from '../api/types'
import { formatSlot, serviceName } from '../format'

// «Записи» tab of the operator queue pane: GET /api/appointments every 10 s while the tab and the page are visible.
export function OperatorAppointments({ onCount }: { onCount?: (count: number) => void }) {
  const { t, i18n } = useTranslation()
  const language = (['ru', 'kk', 'en'].includes(i18n.language) ? i18n.language : 'ru') as Language
  const [items, setItems] = useState<Appointment[] | null>(null)
  const [catalog, setCatalog] = useState<Catalog | null>(null)
  const [error, setError] = useState('')
  const [cancelling, setCancelling] = useState('')

  useEffect(() => { api.catalog().then(setCatalog).catch(() => {}) }, [])
  useEffect(() => {
    let live = true
    const refresh = () => {
      if (document.hidden) return
      api.appointments().then(list => { if (live) { setItems(list); setError('') } }).catch(e => { if (live) setError(e instanceof ApiError && e.serverText ? e.serverText : t('operator.errorAppointments')) })
    }
    refresh()
    const timer = window.setInterval(refresh, 10000)
    document.addEventListener('visibilitychange', refresh)
    return () => { live = false; window.clearInterval(timer); document.removeEventListener('visibilitychange', refresh) }
  }, [])
  useEffect(() => { if (items) onCount?.(items.filter(item => item.status === 'booked').length) }, [items])

  async function cancel(id: string) {
    setCancelling(id); setError('')
    try { const updated = await api.cancelAppointment(id); setItems(current => current?.map(item => item.id === id ? { ...item, ...updated } : item) ?? null) }
    catch (e) { setError(e instanceof ApiError && e.serverText ? e.serverText : t('operator.errorCancelAppointment')) }
    finally { setCancelling('') }
  }
  const service = (id: string) => { const found = catalog?.services.find(item => item.id === id); return found ? serviceName(found, language) : id }
  const doctor = (id: string) => catalog?.doctors.find(item => item.id === id)?.name ?? id

  return <div className="appointment-list">
    {error && <p className="appointments-error" role="alert">{error}</p>}
    {items?.map(item => <article className={`appointment-row${item.status === 'cancelled' ? ' is-cancelled' : ''}`} key={item.id}>
      <div className="appointment-top"><strong>{service(item.service_id)}</strong><span className={`appointment-status status-${item.status}`}>{item.status === 'cancelled' ? t('operator.statusCancelled') : t('operator.statusBooked')}</span></div>
      <p className="appointment-when"><time dateTime={item.slot}>{formatSlot(item.slot, language)}</time> · {doctor(item.doctor_id)}</p>
      <div className="appointment-bottom"><div className="appointment-patient"><span>{item.patient_name}</span><a href={`tel:${item.phone.replace(/[^\d+]/g, '')}`}>{item.phone}</a></div>{item.status === 'booked' && <button type="button" className="appointment-cancel" disabled={cancelling === item.id} onClick={() => void cancel(item.id)}>{cancelling === item.id ? t('operator.cancellingAppointment') : t('operator.cancelAppointment')}</button>}</div>
    </article>)}
    {items && !items.length && <p className="queue-empty">{t('operator.emptyAppointments')}</p>}
    {!items && !error && <p className="queue-empty">…</p>}
  </div>
}
