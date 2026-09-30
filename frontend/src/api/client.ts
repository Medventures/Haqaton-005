import type { ChatResponse, DialogResponse, Ticket, UploadResponse } from './types'
import { mock } from './mock'

const useMock = import.meta.env.VITE_USE_MOCK === 'true'
// Patient and operator tokens live under separate keys: both pages share one origin, and an operator
// token on the patient page made /api/chat answer 403.
export const tokenKey = () => (window.location.pathname.replace(/\/$/, '') === '/operator' ? 'operator_jwt' : 'patient_jwt')
async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const token = localStorage.getItem(tokenKey())
  const response = await fetch(`/api${path}`, { ...init, headers: { ...(init?.body instanceof FormData ? {} : { 'Content-Type': 'application/json' }), ...(token ? { Authorization: `Bearer ${token}` } : {}), ...init?.headers } })
  if (!response.ok) {
    const body = await response.json().catch(() => null) as { error?: string; detail?: string } | null
    throw new Error(body?.detail || body?.error || `Сервер вернул ${response.status}`)
  }
  return response.json() as Promise<T>
}
// Attachments are served behind the same Bearer token, so <img src> cannot load them directly:
// fetch the bytes with the header and let the caller turn them into an object URL.
async function blob(path: string): Promise<Blob> {
  const token = localStorage.getItem(tokenKey())
  const response = await fetch(`/api${path}`, { headers: token ? { Authorization: `Bearer ${token}` } : {} })
  if (!response.ok) throw new Error(`Сервер вернул ${response.status}`)
  return response.blob()
}
function uploadForm(file: File, dialog_id?: string) {
  const form = new FormData()
  form.append('file', file)
  if (dialog_id) form.append('dialog_id', dialog_id)
  return form
}
export const api = {
  patient: () => useMock ? mock.patient() : request<{ token: string; user_id: string; role: 'patient' }>('/auth/patient', { method: 'POST' }),
  operatorLogin: (username: string, password: string) => useMock ? mock.operatorLogin() : request<{ token: string; role: 'operator' }>('/auth/login', { method: 'POST', body: JSON.stringify({ username, password }) }),
  chat: (message: string, dialog_id?: string) => useMock ? mock.chat(message, dialog_id) : request<ChatResponse>('/chat', { method: 'POST', body: JSON.stringify({ message, ...(dialog_id ? { dialog_id } : {}) }) }),
  dialog: (id: string) => useMock ? mock.dialog(id) : request<DialogResponse>(`/dialogs/${encodeURIComponent(id)}`),
  handoff: (dialog_id: string) => useMock ? mock.handoff(dialog_id) : request<{ ticket_id: number; status: string; reply: import('./types').Message }>('/chat/operator', { method: 'POST', body: JSON.stringify({ dialog_id }) }),
  queue: () => useMock ? mock.queue() : request<Ticket[]>('/operator/queue'),
  upload: (file: File, dialog_id?: string) => useMock ? mock.upload(file, dialog_id) : request<UploadResponse>('/chat/attachments', { method: 'POST', body: uploadForm(file, dialog_id) }),
  attachmentBlob: (id: string) => useMock ? mock.attachmentBlob(id) : blob(`/attachments/${encodeURIComponent(id)}`),
  reply: (dialog_id: string, message?: string, close = false) => useMock ? mock.reply(dialog_id, message, close) : request<{ ok?: boolean }>('/operator/reply', { method: 'POST', body: JSON.stringify({ dialog_id, ...(message ? { message } : {}), ...(close ? { close: true } : {}) }) }),
}
