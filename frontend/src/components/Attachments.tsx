import { useEffect, useState } from 'react'
import { FileText } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { api } from '../api/client'
import type { Attachment, Message } from '../api/types'

export const ATTACHMENT_TYPES = ['image/jpeg', 'image/png', 'image/webp', 'application/pdf']
export const ATTACHMENT_MAX_BYTES = 10 * 1024 * 1024
export const isAllowedAttachment = (file: File) => ATTACHMENT_TYPES.includes(file.type) && file.size > 0 && file.size <= ATTACHMENT_MAX_BYTES
export const attachmentsOf = (message: Message): Attachment[] => message.data?.attachments || []

export function formatSize(size: number, language: string) {
  const en = language === 'en'
  if (size < 1024 * 1024) return `${Math.max(1, Math.round(size / 1024))} ${en ? 'KB' : 'КБ'}`
  return `${(size / 1024 / 1024).toFixed(1).replace('.', en ? '.' : ',')} ${en ? 'MB' : 'МБ'}`
}

export function AttachmentList({ attachments }: { attachments: Attachment[] }) {
  return <div className="attachment-list">{attachments.map(attachment => <AttachmentItem key={attachment.id} attachment={attachment} />)}</div>
}

// The file is fetched with the Authorization header and shown through an object URL, revoked on unmount.
function AttachmentItem({ attachment }: { attachment: Attachment }) {
  const { i18n } = useTranslation()
  const [url, setUrl] = useState('')
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    let live = true
    let created = ''
    setUrl(''); setFailed(false)
    api.attachmentBlob(attachment.id)
      .then(blob => { created = URL.createObjectURL(blob); if (live) setUrl(created); else URL.revokeObjectURL(created) })
      .catch(() => { if (live) setFailed(true) })
    return () => { live = false; if (created) URL.revokeObjectURL(created) }
  }, [attachment.id])

  const isImage = attachment.content_type.startsWith('image/')
  if (isImage && !failed) {
    return url
      ? <a className="attachment-thumb" href={url} target="_blank" rel="noopener" title={attachment.name}><img src={url} alt={attachment.name} /></a>
      : <span className="attachment-thumb attachment-thumb-loading" aria-label={attachment.name} />
  }
  const body = <><FileText size={20} strokeWidth={1.75} aria-hidden="true" /><span className="attachment-file-text"><span className="attachment-file-name">{attachment.name}</span><span className="attachment-file-size">{formatSize(attachment.size, i18n.language)}</span></span></>
  return url && !failed
    ? <a className="attachment-file" href={url} target="_blank" rel="noopener">{body}</a>
    : <span className={`attachment-file ${failed ? 'attachment-file-failed' : 'attachment-file-loading'}`}>{body}</span>
}
