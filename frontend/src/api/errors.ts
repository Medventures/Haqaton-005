// status lets callers tell «slot already taken» (409) from a validation error (400)
export class ApiError extends Error {
  status: number
  serverText: string
  constructor(message: string, status: number, serverText = '') {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.serverText = serverText
  }
}
