import type { ChatResponse, DialogResponse, Ticket } from './types'
const dialogs = new Map<string, DialogResponse>()
const services = [{ id: 'therapy', specialty_id: 'therapy', name: 'Приём терапевта', price: 8000, description: 'Первичная консультация специалиста.' }]
const doctors = [{ id: 'doctor-1', specialty_id: 'therapy', name: 'Айдана Сәрсенова', slots: ['2026-10-01T09:00', '2026-10-01T11:30'] }]
const wait = (ms = 700) => new Promise(resolve => setTimeout(resolve, ms))
export const mock = {
  async patient() { await wait(150); return { token: 'mock-patient-token', user_id: 'demo-patient', role: 'patient' as const } },
  async operatorLogin() { return { token: 'mock-operator-token', role: 'operator' as const } },
  async chat(message: string, id?: string): Promise<ChatResponse> {
    await wait()
    const dialog_id = id || crypto.randomUUID(), text = message.toLowerCase()
    const urgency = /кеуде|ентігу|груд|дыхани/.test(text) ? 'red' : /қызу|температур|басым|голов/.test(text) ? 'yellow' : 'green'
    const reply = urgency === 'red' ? 'Срочно позвоните 103. Не ждите ответа в чате.' : urgency === 'yellow' ? 'Рекомендуем обратиться за медицинской оценкой в ближайшее время. Сколько дней сохраняются симптомы?' : /қазақ|сәлем|ауырып|ауыр/.test(text) ? 'Түсіндім. Белгілеріңіз қашан басталды? Тағы не мазалайды?' : 'Понял вас. Подскажите, как давно это началось?'
    const actions = urgency === 'red' ? ['call_103', 'contact_operator'] : urgency === 'yellow' ? ['contact_operator'] : []
    const botMessage = { role: 'bot' as const, content: reply, data: { urgency: urgency as ChatResponse['urgency'], actions, services: urgency === 'green' ? services : [], doctors: urgency === 'green' ? doctors : [] } }
    const prev = dialogs.get(dialog_id) || { dialog: { id: dialog_id, status: 'bot' as const, language: 'ru' as const, urgency: 'green' as const }, messages: [] }
    const next: DialogResponse = { ...prev, dialog: { ...prev.dialog, urgency: urgency as ChatResponse['urgency'], summary: message }, messages: [...prev.messages, { role: 'patient', content: message }, botMessage] }
    dialogs.set(dialog_id, next)
    return { dialog_id, status: 'bot', urgency: urgency as ChatResponse['urgency'], language: /[әғқңөұүһі]/i.test(message) ? 'kk' : 'ru', reply: botMessage, actions, services: urgency === 'green' ? services : [], doctors: urgency === 'green' ? doctors : [] }
  },
  async dialog(id: string) { await wait(100); return dialogs.get(id) || { dialog: { id, status: 'bot' as const, language: 'ru' as const, urgency: 'green' as const }, messages: [] } },
  async handoff(id: string) { const d = dialogs.get(id); if (d) d.dialog.status = 'operator'; return { ticket_id: 1, status: 'open', reply: { role: 'bot' as const, content: 'Обращение передано оператору.' } } },
  async queue(): Promise<Ticket[]> { return [...dialogs.values()].filter(d => d.dialog.status === 'operator').map((d, i) => ({ id: i + 1, dialog_id: d.dialog.id, reason: 'Запрошен оператор', summary: d.dialog.summary || '', status: 'open', urgency: d.dialog.urgency, last_message: d.messages.at(-1)?.content })) },
  async reply(id: string, message?: string, close = false) { const d = dialogs.get(id); if (d && message) d.messages.push({ role: 'operator', content: message }); if (d && close) d.dialog.status = 'bot'; return { ok: true } },
}
