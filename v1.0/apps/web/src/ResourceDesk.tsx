import { useRef, useState, type FormEvent } from 'react'
import { api, ApiError, type Merchant } from './api'
import { bindingCaption, displayTime, errorMessage, useServerRead, type Resources, type TargetVersion } from './desk-data'
import { ErrorNotice, Field, Modal } from './ui'

type Editor = { kind: 'enrollment' | 'virtual_account' } | { kind: 'target'; target?: TargetVersion }
type Props = { merchant: Merchant; base: string; resources: Resources; onChange: () => void }

export function ResourceDesk(props: Props) {
  const { resources, onChange } = props
  const [history, setHistory] = useState<TargetVersion | null>(null)
  const [editor, setEditor] = useState<Editor | null>(null)
  const enrollments = resources.bindings.filter(b => b.kind === 'enrollment')
  return <div className="desk-stack">
    <section className="panel desk-panel">
      <div className="desk-heading">
        <h2>开户与 VA 记录</h2>
        <div className="desk-actions">
          <button className="button secondary" onClick={() => setEditor({ kind: 'enrollment' })}>登记开户资料</button>
          <button className="button secondary" disabled={!enrollments.length} onClick={() => setEditor({ kind: 'virtual_account' })}>登记 VA</button>
        </div>
      </div>
      <p className="desk-hint">登记已有沙盒资料供核验。VA 须关联本商户的开户记录。</p>
      {resources.bindings.length === 0 ? <p className="desk-empty">尚无开户或 VA 记录。</p> : <ul className="desk-rows">
        {resources.bindings.map(binding => <li key={binding.id}>
          <div><strong>{binding.label}</strong><span>{binding.kind === 'enrollment' ? '开户' : 'VA'} · {binding.provider_code} / {binding.instance_code} · {binding.asset || '沙盒'}</span></div>
          <div><code>{binding.masked_external_id}</code><span>资料 V{binding.profile_version} · 待核验</span></div>
        </li>)}
      </ul>}
    </section>
    <section className="panel desk-panel">
      <div className="desk-heading"><h2>收款目标</h2><button className="button secondary" onClick={() => setEditor({ kind: 'target' })}>新增收款目标</button></div>
      {resources.targets.length === 0 ? <p className="desk-empty">该商户尚未登记银行账户或链上地址。</p> : <ul className="desk-rows">
        {resources.targets.map(target => <li key={target.id}>
          <div><strong>{target.label}</strong><span>{target.kind === 'bank' ? '银行账户' : '链上地址'} · {[target.asset, target.network].filter(Boolean).join(' / ')} · V{target.version}</span></div>
          <div><code>{target.masked_destination}</code><span>待复核</span></div>
          <div className="desk-actions"><button className="text-button" onClick={() => setHistory(target)}>版本记录</button>
            <button className="text-button" onClick={() => setEditor({ kind: 'target', target })}>新增版本</button></div>
        </li>)}
      </ul>}
    </section>
    <section className="panel desk-panel">
      <h2>商户资料版本</h2>
      <ul className="desk-rows">{resources.profiles.map(profile => <li key={profile.version}>
        <div><strong>V{profile.version} · {profile.legal_name}</strong><span>{displayTime(profile.created_at)}</span></div>
      </li>)}</ul>
    </section>
    {history && <TargetHistory base={props.base} target={history} close={() => setHistory(null)} />}
    {editor && <ResourceEditor {...props} editor={editor} onClose={() => setEditor(null)} onChange={() => { setEditor(null); onChange() }} />}
  </div>
}

function TargetHistory({ base, target, close }: { base: string; target: TargetVersion; close: () => void }) {
  const versions = useServerRead<{ items: TargetVersion[] }>(`${base}/targets/${encodeURIComponent(target.target_id)}/versions`)
  return <Modal title="收款目标版本记录" onClose={close}>
    {versions.loading && <p role="status">正在读取版本…</p>}
    <ErrorNotice error={versions.error} />
    {versions.error && <button className="text-button" onClick={versions.reload}>重新读取</button>}
    {versions.data && <ul className="desk-rows">{versions.data.items.map(t => <li key={t.id}><div>
      <strong>V{t.version} · {t.label}</strong><span>{[t.asset, t.network, t.masked_destination].filter(Boolean).join(' · ')}</span>
      <span>{displayTime(t.created_at)}</span>
    </div></li>)}</ul>}
  </Modal>
}

function ResourceEditor({ base, editor, merchant, resources, onClose, onChange }: Props & { editor: Editor; onClose: () => void }) {
  const prior = editor.kind === 'target' ? editor.target : undefined
  const [targetKind, setTargetKind] = useState<'bank' | 'chain'>(prior?.kind || 'bank')
  const [parent, setParent] = useState('')
  const [uncertain, setUncertain] = useState(false)
  const [conflict, setConflict] = useState(false)
  const targetAttempt = useRef<Record<string, unknown> | null>(null)
  const [busy, setBusy] = useState(false), [error, setError] = useState('')
  const sending = useRef(false)
  const isTarget = editor.kind === 'target', isVA = editor.kind === 'virtual_account'
  const title = isTarget ? (prior ? '新增收款目标版本' : '新增收款目标') : (isVA ? '登记 VA' : '登记开户资料')
  const enrollments = resources.bindings.filter(b => b.kind === 'enrollment')
  const enrollment = enrollments.find(b => b.id === parent)

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (sending.current) return
    sending.current = true; setBusy(true); setError('')
    const fields = new FormData(event.currentTarget)
    const read = (key: string) => String(fields.get(key) || '').trim()
    try {
      if (isTarget) {
        const body = targetAttempt.current || {
          request_id: crypto.randomUUID(), kind: targetKind, label: read('label'), destination: read('destination'),
          asset: targetKind === 'bank' ? (prior?.asset || resources.fiat_asset) : 'USDT', network: targetKind === 'chain' ? read('network') : '',
          evidence_ref: read('evidence'), ...(prior && { expected_version: prior.version }),
        }
        targetAttempt.current = body
        await api(prior ? `${base}/targets/${encodeURIComponent(prior.target_id)}/versions` : `${base}/targets`, 'POST', body)
      } else {
        await api(`${base}/bindings`, 'POST', {
          kind: editor.kind, label: read('label'), external_id: read('external_id'), evidence_ref: read('evidence'),
          provider_code: enrollment?.provider_code || read('provider'), instance_code: enrollment?.instance_code || read('instance'),
          asset: isVA ? read('asset') : '', account_scope: isVA ? '' : read('scope'),
          profile_version: merchant.row_version, ...(isVA && { parent_binding_id: parent }),
        })
      }
      onChange()
    } catch (err) {
      if (isTarget && (!(err instanceof ApiError) || err.status >= 500)) {
        setUncertain(true)
        setError('尚未收到保存结果。请用原请求重试，或返回收款目标列表核对。')
      } else {
        targetAttempt.current = null
        setUncertain(false)
        setConflict(err instanceof ApiError && err.status === 409)
        setError(errorMessage(err))
      }
    }
    finally { sending.current = false; setBusy(false) }
  }

  return <Modal title={title} onClose={() => { if (!sending.current) onClose() }}>
    <form onSubmit={submit}>
      <fieldset className="desk-fields" disabled={busy || uncertain}>
        <Field label="名称"><input name="label" defaultValue={prior?.label} required maxLength={80} /></Field>
        {isTarget ? <>
          <Field label="目标类型"><select value={targetKind} disabled={Boolean(prior)} onChange={e => setTargetKind(e.target.value as 'bank' | 'chain')}>
            <option value="bank">银行账户 · {prior?.kind === 'bank' ? prior.asset : resources.fiat_asset}</option><option value="chain">链上地址 · USDT</option>
          </select></Field>
          {targetKind === 'chain' && <Field label="网络" hint="按客户确认材料填写完整网络名称。"><input name="network" defaultValue={prior?.network} required /></Field>}
          <Field label={targetKind === 'bank' ? '完整银行收款资料' : '完整链上地址'} hint={prior ? '请重新输入本版本的完整收款目标。旧版本仍保留在原订单。' : undefined}>
            <input name="destination" required maxLength={512} autoComplete="off" />
          </Field>
        </> : <>
          {isVA ? <>
            <Field label="关联开户记录"><select value={parent} onChange={e => setParent(e.target.value)} required>
              <option value="">请选择</option>{enrollments.map(b => <option key={b.id} value={b.id}>{bindingCaption(b)}</option>)}
            </select></Field>
            <Field label="VA 币种"><select name="asset" required defaultValue=""><option value="">请选择</option><option>USD</option><option>HKD</option></select></Field>
          </> : <>
            <div className="desk-form-grid"><Field label="服务商代码"><input name="provider" required maxLength={64} pattern="[a-z0-9][a-z0-9._\-]{0,63}" title="使用小写英文、数字、点、下划线或短横线" autoCapitalize="none" /></Field>
              <Field label="沙盒实例代码"><input name="instance" required maxLength={64} pattern="[a-z0-9][a-z0-9._\-]{0,63}" title="使用小写英文、数字、点、下划线或短横线" autoCapitalize="none" /></Field></div>
            <Field label="外部账户范围" hint="填写服务商侧用于区分该客户或账户的标识。"><input name="scope" required autoComplete="off" /></Field>
          </>}
          <Field label={isVA ? '外部 VA 标识' : '外部开户标识'}><input name="external_id" required autoComplete="off" /></Field>
        </>}
        <Field label="证据引用" hint="填写已保存的回执、邮件或资料编号。"><input name="evidence" required autoComplete="off" /></Field>
      </fieldset>
      <ErrorNotice error={error} />
      {conflict && <button type="button" className="text-button" onClick={onChange}>放弃本次修改并重读资料</button>}
      <div className="form-actions"><button type="button" className="button secondary" disabled={busy} onClick={onClose}>取消</button>
        <button className="button primary" disabled={busy}>{busy ? '正在保存…' : uncertain ? '重试原请求' : '保存记录'}</button></div>
    </form>
  </Modal>
}
