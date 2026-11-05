export type Role = 'operator' | 'finance' | 'supervisor' | 'eo' | 'admin' | 'root'
export const roleNames: Record<Role, string> = { operator: '运营', finance: '财务', supervisor: '主管', eo: 'EO', admin: '管理员', root: '系统管理员' }
export type Staff = { id: string; login_name: string; display_name: string; roles: Role[]; status: string }
export type Merchant = { id: string; merchant_code: string; legal_name: string; contact_name: string; contact_email: string; service_status: string; row_version: number; created_at: string }
export class ApiError extends Error {
  status: number
  code: string
  constructor(status: number, code: string, message: string) { super(message); this.status = status; this.code = code }
}
let csrf = ''
export function clearSession() { csrf = '' }
export function setCSRF(value: string) { csrf = value }
const root = import.meta.env.VITE_API_BASE || `${import.meta.env.BASE_URL}api/v1`
export async function api<T>(path: string, method = 'GET', body?: unknown): Promise<T> {
  const response = await fetch(`${root}${path}`, {
    method, credentials: 'same-origin', cache: 'no-store',
    headers: { ...(method !== 'GET' ? { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf } : {}) },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const data = await response.json().catch(() => ({}))
  if (response.status === 401 && csrf && data.error?.code === 'unauthenticated') window.dispatchEvent(new Event('kaloq-session-expired'))
  if (!response.ok) throw new ApiError(response.status, data.error?.code || 'request_failed', data.error?.message || '操作未完成，请稍后重试。')
  return data as T
}
