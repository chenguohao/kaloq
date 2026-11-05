import { useCallback, useEffect, useRef, useState, type FormEvent, type ReactNode } from 'react'
import QRCode from 'qrcode'
import { passkeyAvailable, registerCredential, verifyCredential, type CreationJSON, type RequestJSON } from './webauthn'
import { api, ApiError, clearSession, setCSRF, roleNames, type Staff, type Merchant, type Role } from './api'

const names: Record<string, string> = { draft: '待完善', active: '服务中', paused: '已暂停', disabled: '已停用' }
const initialMerchant = { legal_name: '', contact_name: '', contact_email: '' }
function Icon({ name, size = 20 }: { name: string; size?: number }) {
  const paths: Record<string, ReactNode> = {
    grid: <><rect x="3" y="3" width="7" height="7" rx="1.4" /><rect x="14" y="3" width="7" height="7" rx="1.4" /><rect x="3" y="14" width="7" height="7" rx="1.4" /><rect x="14" y="14" width="7" height="7" rx="1.4" /></>,
    shield: <><path d="m12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6l8-3Z" /><path d="m8 12 3 3 5-6" /></>,
    users: <><circle cx="9" cy="8" r="3" /><path d="M3 21v-3a6 6 0 0 1 12 0v3M17 5a3 3 0 0 1 0 6m2 10v-3a6 6 0 0 0-2-4" /></>,
    arrow: <><path d="M4 12h16m-6-6 6 6-6 6" /></>,
    plus: <path d="M12 5v14M5 12h14" />,
    search: <><circle cx="10" cy="10" r="6" /><path d="m15 15 6 6" /></>,
    lock: <><rect x="5" y="10" width="14" height="11" rx="2" /><path d="M8 10V7a4 4 0 0 1 8 0v3M12 14v3" /></>,
    exit: <><path d="M9 4H4v16h5m6-13 5 5-5 5M8 12h12" /></>,
    building: <><path d="M4 21V4h12v17m0-10h4v10M8 8h4M8 12h4M8 16h4M2 21h20" /></>,
    check: <path d="m5 12 4 4L19 6" />,
  }
  return <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{paths[name] || paths.grid}</svg>
}
function Brand({ light = false }: { light?: boolean }) { return <div className={`brand ${light ? 'light' : ''}`}><span>kaloq<span className="brand-dot">.</span></span></div> }
function ErrorNotice({ error }: { error: string }) { return error ? <div role="alert" className="notice error">{error}</div> : null }
function Field({ label, children, hint }: { label: string; children: ReactNode; hint?: string }) { return <div className="field"><label><span>{label}</span>{children}</label>{hint && <small>{hint}</small>}</div> }
function Badge({ status }: { status: string }) { return <span className={`badge ${status}`}>{names[status] || status}</span> }
function humanError(error: unknown) {
  if (error instanceof DOMException && error.name === 'NotAllowedError') return '凭证操作已取消或超时，请再次尝试。'
  if (error instanceof DOMException && error.name === 'InvalidStateError') return '此凭证已登记，请使用现有凭证或选择其他凭证。'
  if (error instanceof ApiError) {
    if (error.status === 401 && error.code === 'invalid_code') return error.message
    if (error.status === 401) return '验证未通过或会话已过期，请检查后重新登录。'
    if (error.status === 403) return '当前账号无权进行此操作，或安全验证已失效。'
    if (error.status === 409) return '资料已发生变化，请刷新后重新提交。'
    if (error.status === 429) return '尝试次数较多，请稍后再试。'
    return error.message
  }
  return error instanceof TypeError ? '无法连接服务，请检查网络后重试。' : error instanceof Error ? error.message : '操作未完成，请重试。'
}
function useWork() {
  const [busy, setBusy] = useState(false), [error, setError] = useState('')
  const run = async (fn: () => Promise<unknown>) => { setBusy(true); setError(''); try { await fn() } catch (e) { setError(humanError(e)) } finally { setBusy(false) } }
  return { busy, error, setError, run }
}
function CodeInput({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return <input aria-label="六位动态码" className="code-input" inputMode="numeric" autoComplete="one-time-code" pattern="[0-9]{6}" maxLength={6} minLength={6} placeholder="000000" value={value} onChange={e => onChange(e.target.value.replace(/\D/g, ''))} required />
}

function Login({ onAuthenticated }: { onAuthenticated: () => Promise<void> }) {
  const [stage, setStage] = useState<'password' | 'totp' | 'enroll' | 'recover' | 'codes'>('password')
  const [loginName, setLoginName] = useState(''), [password, setPassword] = useState(''), [code, setCode] = useState(''), [recoveryCode, setRecoveryCode] = useState('')
  const [enrollment, setEnrollment] = useState<{ secret: string; otpauth_uri: string } | null>(null)
  const [qr, setQR] = useState(''), [codes, setCodes] = useState<string[]>([]), [saved, setSaved] = useState(false)
  const { busy, error, run, setError } = useWork()
  useEffect(() => { let disposed = false; setQR(''); if (enrollment) QRCode.toDataURL(enrollment.otpauth_uri, { width: 204, margin: 1, color: { dark: '#102820', light: '#ffffff' } }).then(v => { if (!disposed) setQR(v) }).catch(() => { if (!disposed) setError('二维码生成失败，可使用下方手动密钥完成绑定。') }); return () => { disposed = true } }, [enrollment, setError])
  async function loadEnrollment() { setStage('enroll'); setEnrollment(await api('/auth/enrollment')) }
  async function submit(e: FormEvent) {
    e.preventDefault()
    await run(async () => {
      if (stage === 'password') {
        const r = await api<{ stage: 'totp' | 'enroll' }>('/auth/login', 'POST', { login_name: loginName, password })
        setPassword(''); setCode('')
        if (r.stage === 'enroll') await loadEnrollment(); else setStage('totp')
      } else if (stage === 'totp') { await api('/auth/totp', 'POST', { code }); setCode(''); await onAuthenticated() }
      else if (stage === 'enroll') {
        const r = await api<{ recovery_codes: string[] }>('/auth/enrollment', 'POST', { code })
        setCode(''); setEnrollment(null); setQR(''); setCodes(r.recovery_codes); setStage('codes')
      } else if (stage === 'recover') { await api('/auth/recover', 'POST', { recovery_code: recoveryCode }); setRecoveryCode(''); await loadEnrollment() }
      else { setCodes([]); await onAuthenticated() }
    })
  }
  const titles = { password: '登录工作台', totp: '验证您的身份', enroll: '绑定身份认证器', recover: '恢复身份验证', codes: '保存您的恢复码' }
  return <div className="login-layout"><aside className="login-story"><Brand light /><div className="story-content"><span className="eyebrow">INTERNAL WORKSPACE</span><h1>每一笔业务，<br />都有清晰的起点。</h1><p>从商户建档开始，<br />让协作、记录与责任有据可循。</p><div className="story-graphic"><div className="orbit one" /><div className="orbit two" /><div className="orbit three" /><div className="orbit-dot" /><span className="graphic-center"><Icon name="building" size={48} /></span></div></div><div className="story-footer"><span>内部员工专用</span><span>1.0 · 沙盒验收环境</span></div></aside><main className="login-main"><div className="mobile-brand"><Brand /></div><section className="login-card"><div className="section-icon"><Icon name={stage === 'password' ? 'lock' : 'shield'} /></div><div className="eyebrow dark">KALOQ WORKSPACE</div><h2>{titles[stage]}</h2><p className="muted intro">{stage === 'password' ? '使用您的员工账号，继续今天的工作。' : stage === 'totp' ? '输入 Google Authenticator 中当前的六位动态码。' : stage === 'enroll' ? '使用 Google Authenticator 扫码，或手动输入密钥。' : stage === 'recover' ? '使用预先保存的恢复码，重新绑定认证器。' : '恢复码仅展示这一次，请保存在安全的位置。'}</p>
      <ErrorNotice error={error} /><form onSubmit={submit}>
        {stage === 'password' && <><Field label="员工账号"><input autoComplete="username" autoFocus value={loginName} onChange={e => setLoginName(e.target.value)} required maxLength={100} placeholder="输入账号" /></Field><Field label="密码"><input type="password" autoComplete="current-password" value={password} onChange={e => setPassword(e.target.value)} required placeholder="输入密码" /></Field></>}
        {stage === 'enroll' && <>{enrollment ? <div className="enrollment-box">{qr && <img className="qr" src={qr} alt="Google Authenticator 绑定二维码" />}<details><summary>无法扫码？查看手动密钥</summary><code className="secret">{enrollment.secret}</code></details></div> : <button type="button" className="button secondary" disabled={busy} onClick={() => run(loadEnrollment)}>加载绑定信息</button>}</>}
        {(stage === 'totp' || stage === 'enroll') && <Field label="动态码"><CodeInput value={code} onChange={setCode} /></Field>}
        {stage === 'recover' && <><div className="notice">恢复后，旧认证器和登录会话会失效。重新绑定完成后才能进入工作台。</div><Field label="一次性恢复码"><input autoComplete="off" value={recoveryCode} onChange={e => setRecoveryCode(e.target.value)} required placeholder="输入保存的恢复码" /></Field></>}
        {stage === 'codes' && <><div className="recovery-codes">{codes.map(v => <code key={v}>{v}</code>)}</div><label className="checkbox"><input type="checkbox" checked={saved} onChange={e => setSaved(e.target.checked)} required /><span>我已将恢复码保存在安全的位置</span></label></>}
        <button className="button primary full" disabled={busy || (stage === 'codes' && !saved) || (stage === 'enroll' && !enrollment)}>{busy ? '正在处理…' : stage === 'password' ? '继续验证' : stage === 'codes' ? '进入工作台' : stage === 'recover' ? '验证并重新绑定' : '验证并继续'}<Icon name="arrow" size={17} /></button>
      </form>
      {stage === 'totp' && <button className="text-button centered" onClick={() => { setStage('recover'); setError(''); setCode('') }}>无法使用认证器？使用恢复码</button>}
      {(stage === 'enroll' || stage === 'totp' || stage === 'recover') && <button className="text-button centered" disabled={busy} onClick={() => run(async () => { await api('/auth/logout', 'POST', {}); setStage('password'); setEnrollment(null); setQR(''); setCode(''); setRecoveryCode('') })}>返回账号登录</button>}
      {stage === 'recover' && <p className="small muted">没有恢复码时，请联系系统维护人员进行身份核验。人工重置授权流程尚未开放。</p>}
      <p className="login-footnote"><Icon name="shield" size={14} />账号仅供本人使用 · 双因素验证保护</p></section></main></div>
}

function MerchantList({ onOpen }: { onOpen: (m: Merchant) => void }) {
  const [items, setItems] = useState<Merchant[]>([]), [search, setSearch] = useState(''), [status, setStatus] = useState(''), [loading, setLoading] = useState(true)
  const [creating, setCreating] = useState(false), [form, setForm] = useState(initialMerchant)
  const { busy, error, run } = useWork()
  const sequence = useRef(0)
  const load = useCallback(async () => { const request = ++sequence.current; setLoading(true); try { const query = new URLSearchParams({ q: search, status }); const r = await api<{ items: Merchant[] }>(`/merchants?${query}`); if (request === sequence.current) setItems(r.items || []) } finally { if (request === sequence.current) setLoading(false) } }, [search, status])
  useEffect(() => { const timer = setTimeout(() => { void run(load) }, 250); return () => { clearTimeout(timer); sequence.current++ } }, [load])
  const filtered = items
  return <><div className="page-heading"><div><div className="eyebrow dark">MERCHANTS</div><h1>商户</h1><p className="muted">查看全部商户，进入工作台办理业务。</p></div><button className="button primary" onClick={() => setCreating(true)}><Icon name="plus" size={18} />创建商户</button></div><ErrorNotice error={error} /><div className="stats-row"><div><span className="muted">当前查询结果</span><strong>{loading ? '—' : items.length}<small>家</small></strong></div><div><span className="muted">服务中</span><strong>{loading ? '—' : items.filter(m => m.service_status === 'active').length}<small>家</small></strong></div><div><span className="muted">待完善</span><strong>{loading ? '—' : items.filter(m => m.service_status === 'draft').length}<small>家</small></strong></div><div className="stats-note"><Icon name="shield" /><p>平台档案独立管理<br /><span>通道开户状态另行核验</span></p></div></div>
    <section className="panel"><div className="table-toolbar"><label className="search-box"><Icon name="search" size={18} /><input aria-label="搜索商户" placeholder="搜索商户名称或编号" value={search} onChange={e => setSearch(e.target.value)} /></label><div className="toolbar-right"><select aria-label="筛选服务状态" value={status} onChange={e => setStatus(e.target.value)}><option value="">全部状态</option><option value="draft">待完善</option><option value="active">服务中</option><option value="paused">已暂停</option></select><button className="text-button" disabled={busy} onClick={() => run(load)}>刷新</button></div></div>
    {loading ? <div className="empty-state">正在加载商户…</div> : !filtered.length ? <div className="empty-state"><span className="empty-icon"><Icon name="building" size={28} /></span><h3>{search || status ? '未找到匹配的商户' : '从第一份商户档案开始'}</h3><p>{search || status ? '试试其他关键词，或调整状态筛选。' : '录入主体与联系人信息，系统会生成唯一商户编号。'}</p>{!search && !status && <button className="button secondary" onClick={() => setCreating(true)}>创建商户档案</button>}</div> : <div className="table-scroll"><table><thead><tr><th>商户名称 / 编号</th><th>联系人</th><th>服务状态</th><th>建档日期</th><th /></tr></thead><tbody>{filtered.map(m => <tr key={m.id}><td><button className="merchant-name" onClick={() => onOpen(m)}>{m.legal_name}</button><span className="subtext mono">{m.merchant_code}</span></td><td>{m.contact_name}<span className="subtext">{m.contact_email}</span></td><td><Badge status={m.service_status} /></td><td className="muted">{new Date(m.created_at).toLocaleDateString('zh-CN')}</td><td><button className="text-button enter-button" onClick={() => onOpen(m)}>进入工作台<Icon name="arrow" size={16} /></button></td></tr>)}</tbody></table></div>}<div className="table-footer">当前显示 {filtered.length} 家商户<span>最多显示 200 条，请搜索缩小范围</span></div></section>
    {creating && <Modal title="创建商户档案" onClose={() => { if (!busy) setCreating(false) }}><p className="muted">创建后生成独立商户编号。通道开户需要后续单独申请。</p><form onSubmit={e => { e.preventDefault(); run(async () => { const r = await api<{ merchant: Merchant }>('/merchants', 'POST', form); setCreating(false); setForm(initialMerchant); onOpen(r.merchant) }) }}><MerchantFields form={form} setForm={setForm} /><ErrorNotice error={error} /><div className="form-actions"><button type="button" className="button secondary" disabled={busy} onClick={() => setCreating(false)}>取消</button><button className="button primary" disabled={busy}>{busy ? '正在创建…' : '创建档案'}</button></div></form></Modal>}
  </>
}
function MerchantFields({ form, setForm }: { form: typeof initialMerchant; setForm: (v: typeof initialMerchant) => void }) { return <><Field label="商户主体名称"><input value={form.legal_name} onChange={e => { e.target.setCustomValidity(new TextEncoder().encode(e.target.value).length > 200 ? '内容超过长度上限，请缩短后重试。' : ''); setForm({ ...form, legal_name: e.target.value }) }} required maxLength={200} placeholder="完整法定名称" /></Field><Field label="联系人姓名"><input value={form.contact_name} onChange={e => { e.target.setCustomValidity(new TextEncoder().encode(e.target.value).length > 100 ? '内容超过长度上限，请缩短后重试。' : ''); setForm({ ...form, contact_name: e.target.value }) }} required maxLength={100} /></Field><Field label="联系邮箱"><input type="email" value={form.contact_email} onChange={e => { e.target.setCustomValidity(new TextEncoder().encode(e.target.value).length > 254 ? '内容超过长度上限，请缩短后重试。' : ''); setForm({ ...form, contact_email: e.target.value }) }} required maxLength={254} /></Field></> }
function Modal({ title, children, onClose }: { title: string; children: ReactNode; onClose: () => void }) {
  const panel = useRef<HTMLElement>(null), close = useRef(onClose)
  close.current = onClose
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null
    const focusable = () => Array.from(panel.current?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),select:not(:disabled),[tabindex="0"]') || [])
    focusable()[0]?.focus()
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') close.current()
      if (e.key === 'Tab') { const items = focusable(), first = items[0], last = items.at(-1); if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last?.focus() } else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first?.focus() } }
    }
    const overflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    document.addEventListener('keydown', handler)
    return () => { document.removeEventListener('keydown', handler); document.body.style.overflow = overflow; previous?.focus() }
  }, [])
  return <div className="modal-backdrop"><section ref={panel} className="modal" role="dialog" aria-modal="true" aria-label={title}><div className="modal-heading"><h2>{title}</h2><button className="close-button" aria-label="关闭" onClick={onClose}>×</button></div>{children}</section></div>
}
function MerchantDetail({ merchant, onBack }: { merchant: Merchant; onBack: () => void }) {
  const [data, setData] = useState(merchant), [editing, setEditing] = useState(false), [form, setForm] = useState(initialMerchant), [status, setStatus] = useState(merchant.service_status)
  const { busy, error, run } = useWork()
  const refresh = async () => { const r = await api<{ merchant: Merchant }>(`/merchants/${merchant.id}`); setData(r.merchant); return r.merchant }
  useEffect(() => { void run(refresh) }, [merchant.id])
  return <><button className="text-button back-button" onClick={onBack}>← 全部商户</button><div className="page-heading"><div><div className="eyebrow dark">MERCHANT WORKSPACE</div><h1>{data.legal_name}</h1><p className="muted mono">{data.merchant_code}</p></div><Badge status={data.service_status} /></div><ErrorNotice error={error} />
    <section className="panel profile-panel"><div className="panel-heading"><h2>商户档案</h2><button className="text-button" onClick={() => { setForm({ legal_name: data.legal_name, contact_name: data.contact_name, contact_email: data.contact_email }); setStatus(data.service_status); setEditing(true) }}>编辑资料</button></div><dl className="detail-grid"><div><dt>联系人</dt><dd>{data.contact_name || '—'}</dd></div><div><dt>联系邮箱</dt><dd>{data.contact_email || '—'}</dd></div><div><dt>建档时间</dt><dd>{new Date(data.created_at).toLocaleString('zh-CN')}</dd></div><div><dt>平台唯一 ID</dt><dd className="mono small">{data.id}</dd></div></dl></section>
    <div className="section-heading"><h2>商户服务</h2><span className="muted small">按验证进度逐步开放</span></div><div className="service-grid">{[{ title: '资料与通道开户', icon: 'building', text: '完善资料后申请通道账户。平台建档不代表开户已通过。' }, { title: '业务订单', icon: 'grid', text: '在商户内办理业务，跟踪每个阶段的状态与所需材料。' }, { title: '资金与对账', icon: 'shield', text: '查询经核验的资金记录与差异，保持每笔业务可追溯。' }].map(s => <section className="panel service-card" key={s.title}><span className="section-icon"><Icon name={s.icon} /></span><h3>{s.title}</h3><p className="muted">{s.text}</p><span className="closed-label"><Icon name="lock" size={14} />尚未开放</span></section>)}</div>
    {editing && <Modal title="编辑商户档案" onClose={() => { if (!busy) setEditing(false) }}><form onSubmit={e => { e.preventDefault(); run(async () => { const r = await api<{ merchant: Merchant }>(`/merchants/${data.id}`, 'PATCH', { ...form, service_status: status, row_version: data.row_version }); setData(r.merchant); setEditing(false) }) }}><MerchantFields form={form} setForm={setForm} /><Field label="平台服务状态"><select value={status} onChange={e => setStatus(e.target.value)}><option value="draft">待完善</option><option value="active">服务中</option><option value="paused">已暂停</option></select></Field><p className="small muted">此状态仅代表平台服务安排，不代表通道开户或资金动作已开放。</p><ErrorNotice error={error} /><div className="form-actions"><button type="button" className="button secondary" disabled={busy} onClick={() => run(async () => { const m = await refresh(); setForm({ legal_name: m.legal_name, contact_name: m.contact_name, contact_email: m.contact_email }); setStatus(m.service_status) })}>重新读取</button><button className="button primary" disabled={busy}>{busy ? '正在保存…' : '保存修改'}</button></div></form></Modal>}
  </>
}

function StaffPage({ current }: { current: Staff }) {
  const [items, setItems] = useState<Staff[]>([]), [selected, setSelected] = useState<Staff | 'new' | null>(null)
  const [form, setForm] = useState({ login_name: '', display_name: '', password: '', roles: [] as Role[], status: 'active', code: '' })
  const { busy, error, run } = useWork()
  const load = async () => { const r = await api<{ items: Staff[] }>('/staff'); setItems(r.items || []) }
  useEffect(() => { void run(load) }, [])
  function open(staff: Staff | 'new') { setSelected(staff); setForm({ login_name: staff === 'new' ? '' : staff.login_name, display_name: staff === 'new' ? '' : staff.display_name, password: '', roles: staff === 'new' ? [] : staff.roles, status: staff === 'new' ? 'active' : staff.status, code: '' }) }
  return <><div className="page-heading"><div><div className="eyebrow dark">TEAM</div><h1>员工与权限</h1><p className="muted">维护员工账号与角色，每次变更需再次验证身份。</p></div><button className="button primary" onClick={() => open('new')}><Icon name="plus" size={18} />添加员工</button></div><ErrorNotice error={error} /><section className="panel"><div className="table-scroll"><table><thead><tr><th>员工</th><th>角色</th><th>状态</th><th /></tr></thead><tbody>{items.map(s => <tr key={s.id}><td><strong>{s.display_name}</strong><span className="subtext">{s.login_name}</span></td><td><div className="roles">{s.roles.map(r => <span className="role-tag" key={r}>{roleNames[r]}</span>)}</div></td><td><span className={`badge ${s.status}`}>{s.status === 'active' ? '已启用' : '已停用'}</span></td><td>{s.id === current.id ? <span className="small muted">当前账号</span> : s.roles.includes('root') ? <span className="small muted">受控管理</span> : <button className="text-button" onClick={() => open(s)}>调整权限</button>}</td></tr>)}</tbody></table></div>{!items.length && <div className="empty-state">{busy ? '正在加载员工…' : '暂无员工记录'}</div>}</section><p className="small muted">角色变更或停用会使该员工的旧会话失效。维护角色不自动取得商户办理或资金审批权限。</p>
    {selected && <Modal title={selected === 'new' ? '添加员工' : `调整权限 · ${selected.display_name}`} onClose={() => { if (!busy) setSelected(null) }}><form onSubmit={e => { e.preventDefault(); run(async () => { if (selected === 'new') await api('/staff', 'POST', { login_name: form.login_name, display_name: form.display_name, password: form.password, roles: form.roles, code: form.code }); else await api(`/staff/${selected.id}`, 'PATCH', { roles: form.roles, status: form.status, code: form.code }); setSelected(null); setForm({ login_name: '', display_name: '', password: '', roles: [], status: 'active', code: '' }); await load() }) }}>
      {selected === 'new' && <><Field label="员工姓名"><input value={form.display_name} onChange={e => { e.target.setCustomValidity(new TextEncoder().encode(e.target.value).length > 100 ? '内容超过长度上限，请缩短后重试。' : ''); setForm({ ...form, display_name: e.target.value }) }} required maxLength={100} /></Field><Field label="登录账号" hint="3–64 位小写字母、数字、点、下划线或短横线，以字母或数字开头。"><input autoComplete="off" pattern="[a-z0-9][a-z0-9._\-]{2,63}" value={form.login_name} onChange={e => setForm({ ...form, login_name: e.target.value })} required maxLength={64} /></Field><Field label="初始密码" hint="12–72 字节。通过安全渠道交付给员工，首次登录需绑定认证器。"><input type="password" autoComplete="new-password" value={form.password} onChange={e => { e.target.setCustomValidity(new TextEncoder().encode(e.target.value).length > 72 || new TextEncoder().encode(e.target.value).length < 12 ? '密码长度须为 12–72 字节。' : ''); setForm({ ...form, password: e.target.value }) }} required maxLength={72} /></Field></>}
      <fieldset className="roles-field"><legend>角色</legend>{(Object.keys(roleNames) as Role[]).filter(r => r !== 'root').map(r => <label className="checkbox" key={r}><input type="checkbox" checked={form.roles.includes(r)} onChange={e => setForm({ ...form, roles: e.target.checked ? [...form.roles, r] : form.roles.filter(x => x !== r) })} /><span>{roleNames[r]}</span></label>)}</fieldset>
      {selected !== 'new' && <Field label="账号状态"><select value={form.status} onChange={e => setForm({ ...form, status: e.target.value })}><option value="active">启用</option><option value="disabled">停用</option></select></Field>}
      <Field label="您的当前动态码" hint="已使用过的动态码不可重复提交，请等待新动态码。"><CodeInput value={form.code} onChange={code => setForm({ ...form, code })} /></Field><ErrorNotice error={error} /><div className="form-actions"><button type="button" className="button secondary" disabled={busy} onClick={() => setSelected(null)}>取消</button><button className="button primary" disabled={busy || !form.roles.length}>{busy ? '正在保存…' : '确认保存'}</button></div></form></Modal>}
  </>
}

function SecurityPage() {
  return <><div className="page-heading"><div><div className="eyebrow dark">ACCOUNT SECURITY</div><h1>个人安全</h1><p className="muted">管理用于身份验证和个人签名的凭证。</p></div></div><section className="panel security-row"><div className="section-icon"><Icon name="shield" /></div><div className="security-content"><h2>Google Authenticator</h2><p className="muted">登录和敏感操作使用六位动态码验证。请妥善保管绑定时生成的恢复码。</p></div><span className="badge active">已完成验证</span></section><PasskeyCard /><section className="panel security-row"><div className="section-icon"><Icon name="lock" /></div><div className="security-content"><h2>恢复访问</h2><p className="muted">设备遗失时，在密码验证后选择“使用恢复码”，验证后重新绑定认证器。旧会话将失效。</p><p className="small muted">无恢复码的人工重置流程尚未开放，请联系系统维护人员。</p></div></section></>
}
type Passkey = { id: string; label: string; created_at: string; revoked_at?: string; active: boolean }
function PasskeyCard() {
  const [items, setItems] = useState<Passkey[]>([]), [ready, setReady] = useState(false), [action, setAction] = useState<'register' | Passkey | null>(null)
  const [label, setLabel] = useState(''), [code, setCode] = useState(''), [message, setMessage] = useState('')
  const { busy, error, run } = useWork()
  const supported = passkeyAvailable()
  const load = async () => { const r = await api<{ items: Passkey[] }>('/security/passkeys'); setItems(r.items || []); setReady(true) }
  useEffect(() => { void run(load) }, [])
  async function verify() {
    setMessage('')
    const r = await api<{ challenge_id: string; publicKey: RequestJSON }>('/security/passkeys/verify/begin', 'POST', {})
    const credential = await verifyCredential(r.publicKey)
    await api('/security/passkeys/verify/finish', 'POST', { challenge_id: r.challenge_id, credential })
    setMessage('凭证验证通过。本次仅验证凭证可用性，未授权任何资金操作。')
  }
  return <section className="panel passkey-panel"><div className="security-row"><div className="section-icon"><Icon name="lock" /></div><div className="security-content"><h2>个人签名凭证</h2><p className="muted">使用电脑、手机或安全密钥登记个人凭证。资金审批签名尚未开放。</p></div></div><ErrorNotice error={error} />{message && <div className="notice success" role="status">{message}</div>}{!supported && <div className="notice">当前浏览器或连接环境不支持凭证登记。请使用支持通行密钥的浏览器，并通过安全连接访问。</div>}<div className="passkey-list">{items.map(p => <div className="passkey-item" key={p.id}><div><strong>{p.label}</strong><span className="subtext">登记于 {new Date(p.created_at).toLocaleString('zh-CN')}</span></div>{p.active ? <button className="text-button danger" disabled={busy} onClick={() => { setAction(p); setCode(''); setMessage('') }}>撤销</button> : <span className="closed-label">{p.revoked_at ? '已撤销' : '已失效'}</span>}</div>)}{ready && !items.length && <p className="muted small">尚未登记个人凭证。</p>}</div><div className="passkey-actions"><button className="button secondary" disabled={busy || !supported || !ready} onClick={() => { setAction('register'); setCode(''); setLabel(''); setMessage('') }}><Icon name="plus" size={16} />登记新凭证</button><button className="text-button" disabled={busy || !supported || !items.some(p => p.active)} onClick={() => run(verify)}>{busy ? '正在验证…' : '验证凭证可用性'}</button></div>
    {action && <Modal title={action === 'register' ? '登记个人凭证' : '撤销个人凭证'} onClose={() => { if (!busy) setAction(null) }}><form onSubmit={e => { e.preventDefault(); run(async () => { if (action === 'register') { const r = await api<{ challenge_id: string; publicKey: CreationJSON }>('/security/passkeys/register/begin', 'POST', { code, label }); setCode(''); const credential = await registerCredential(r.publicKey); await api('/security/passkeys/register/finish', 'POST', { challenge_id: r.challenge_id, credential }); setMessage('个人凭证已登记，可继续验证其可用性。') } else { await api(`/security/passkeys/${action.id}/revoke`, 'POST', { code }); setCode(''); setMessage('凭证已撤销。') } setAction(null); await load() }) }}>
    {action === 'register' ? <Field label="凭证名称" hint="用于识别凭证，如“工作电脑”或“我的手机”。"><input value={label} onChange={e => setLabel(e.target.value)} required maxLength={80} placeholder="输入易识别的名称" /></Field> : <p className="notice">撤销“{action.label}”后，该凭证将无法用于验证。</p>}<Field label="当前动态码" hint="请使用尚未提交过的动态码。"><CodeInput value={code} onChange={setCode} /></Field><ErrorNotice error={error} /><div className="form-actions"><button type="button" className="button secondary" disabled={busy} onClick={() => setAction(null)}>取消</button><button className="button primary" disabled={busy}>{busy ? '请完成浏览器验证…' : action === 'register' ? '继续登记' : '确认撤销'}</button></div></form></Modal>}
  </section>
}

export default function App() {
  const [user, setUser] = useState<Staff | null>(null), [loading, setLoading] = useState(true), [initialError, setInitialError] = useState('')
  const [page, setPage] = useState<'merchants' | 'staff' | 'security'>('security'), [merchant, setMerchant] = useState<Merchant | null>(null)
  const { busy, error, run } = useWork()
  const loadSession = useCallback(async () => { const r = await api<{ user: Staff; csrf_token: string }>('/me'); setCSRF(r.csrf_token); setUser(r.user); setPage(r.user.roles.includes('operator') ? 'merchants' : r.user.roles.includes('root') ? 'staff' : 'security') }, [])
  useEffect(() => { let live = true; loadSession().catch(e => { if (live && (!(e instanceof ApiError) || e.status !== 401)) setInitialError(humanError(e)) }).finally(() => { if (live) setLoading(false) }); return () => { live = false } }, [loadSession])
  useEffect(() => { const expired = () => { clearSession(); setUser(null); setMerchant(null) }; window.addEventListener('kaloq-session-expired', expired); return () => window.removeEventListener('kaloq-session-expired', expired) }, [])
  useEffect(() => {
    if (!user) return
    const check = () => { if (document.visibilityState !== 'visible') return; api<{ user: Staff; csrf_token: string }>('/me').then(r => { setCSRF(r.csrf_token); setUser(r.user) }).catch(() => {}) }
    const timer = setInterval(check, 60000)
    window.addEventListener('focus', check)
    return () => { clearInterval(timer); window.removeEventListener('focus', check) }
  }, [user?.id])
  if (loading) return <div className="boot"><Brand /><p>正在验证会话…</p></div>
  if (initialError) return <div className="boot"><Brand /><ErrorNotice error={initialError} /><button className="button secondary" onClick={() => location.reload()}>重新连接</button></div>
  if (!user) return <Login onAuthenticated={loadSession} />
  const navigation = [{ id: 'merchants' as const, label: '商户工作台', icon: 'grid', show: user.roles.includes('operator') }, { id: 'staff' as const, label: '员工与权限', icon: 'users', show: user.roles.includes('root') }, { id: 'security' as const, label: '个人安全', icon: 'shield', show: true }].filter(n => n.show)
  return <div className="app-layout"><aside className="sidebar"><Brand light /><div className="sidebar-caption">工作空间</div><nav aria-label="主导航">{navigation.map(n => <button key={n.id} className={`nav-item ${page === n.id ? 'selected' : ''}`} aria-current={page === n.id ? 'page' : undefined} onClick={() => { setPage(n.id); setMerchant(null) }}><Icon name={n.icon} /><span>{n.label}</span>{page === n.id && <span className="nav-dot" />}</button>)}</nav><div className="sidebar-bottom"><span className="environment-dot" /><span>沙盒验收环境</span><small>1.0</small></div></aside><div className="workspace"><header className="topbar"><div className="breadcrumb">工作空间<span>/</span>{navigation.find(n => n.id === page)?.label}</div><div className="account"><span className="avatar">{user.display_name.slice(0, 1)}</span><div><strong>{user.display_name}</strong><small>{user.roles.map(r => roleNames[r]).join(' · ')}</small></div><button title="退出登录" aria-label="退出登录" className="icon-button" disabled={busy} onClick={() => run(async () => { await api('/auth/logout', 'POST', {}); clearSession(); setUser(null); setMerchant(null) })}><Icon name="exit" size={18} /></button></div></header><main className="main-content"><ErrorNotice error={error} />{page === 'merchants' ? merchant ? <MerchantDetail merchant={merchant} onBack={() => setMerchant(null)} /> : <MerchantList onOpen={setMerchant} /> : page === 'staff' ? <StaffPage current={user} /> : <SecurityPage />}</main><footer className="workspace-footer">Kaloq · 内部工作台<span>每一次操作，留有记录。</span></footer></div></div>
}
