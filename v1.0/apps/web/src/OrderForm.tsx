import { useRef, useState, type FormEvent } from 'react'
import { api, ApiError, type Merchant } from './api'
import { bindingCaption, directions, errorMessage, targetCaption, type Direction, type Order, type Resources } from './desk-data'
import { ErrorNotice, Field } from './ui'

function localDateValue(value: string) {
  const date = new Date(value)
  return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, -1)
}

export function OrderForm({ merchant, base, resources, reloadResources, prior, onSaved, onBack }: {
  merchant: Merchant; base: string; resources?: Resources; reloadResources: () => void
  prior?: Order; onSaved: (order: Order) => void; onBack: () => void
}) {
  const [direction, setDirection] = useState<Direction>(prior?.direction || 'fiat_to_crypto')
  const [includeQuote, setIncludeQuote] = useState(Boolean(prior?.quote))
  const [fiatAsset] = useState(prior
    ? prior.direction === 'fiat_to_crypto' ? prior.source_asset : prior.destination_asset
    : resources?.fiat_asset)
  const fiatChanged = !prior && resources && fiatAsset !== resources.fiat_asset
  const [source, destination] = direction === 'fiat_to_crypto' ? [fiatAsset, 'USDT'] : ['USDT', fiatAsset]
  const [busy, setBusy] = useState(false), [error, setError] = useState('')
  const [uncertain, setUncertain] = useState(false)
  const sending = useRef(false)
  // A lost response may follow a committed transaction. Keep the entire attempted payload for exact replay.
  const pending = useRef<Record<string, unknown> | null>(null)
  const targets = resources?.targets.filter(t => t.asset === destination && t.kind === (direction === 'fiat_to_crypto' ? 'chain' : 'bank')) || []
  const bindings = resources?.bindings.filter(b => b.kind === 'enrollment' || (direction === 'fiat_to_crypto' && b.asset === source)) || []
  const profileVersion = resources?.profiles.reduce((latest, p) => Math.max(latest, p.version), merchant.row_version) || merchant.row_version

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (sending.current) return
    const values = new FormData(event.currentTarget)
    const read = (key: string) => String(values.get(key) || '').trim()
    sending.current = true; setBusy(true); setError('')
    try {
      if (!pending.current) {
        const quote = includeQuote ? {
          rate: read('rate'), destination_amount: read('destination_amount'),
          evidence: read('evidence'), expires_at: new Date(read('expires')).toISOString(),
        } : undefined
        pending.current = {
          request_id: crypto.randomUUID(), direction, fiat_asset: fiatAsset, source_amount: read('amount'),
          instruction: String(values.get('instruction') || ''), profile_version: profileVersion,
          ...(read('target') && { target_version_id: read('target') }),
          ...(read('binding') && { channel_binding_id: read('binding') }),
          ...(quote && { quote }), ...(prior && { expected_version: prior.version }),
        }
      }
      const path = `${base}/orders${prior ? `/${encodeURIComponent(prior.id)}/versions` : ''}`
      const result = await api<{ order: Order }>(path, 'POST', pending.current)
      onSaved(result.order)
    } catch (err) {
      if (err instanceof ApiError && err.status < 500) {
        pending.current = null; setUncertain(false); setError(errorMessage(err))
      } else {
        setUncertain(true)
        setError('尚未收到保存结果。请用原请求重试，或返回列表核对订单。')
      }
    } finally { sending.current = false; setBusy(false) }
  }

  return <section className="panel desk-panel">
    <button type="button" className="text-button" onClick={onBack} disabled={busy}>← {prior ? '订单详情' : '订单列表'}</button>
    <h2>{prior ? `修订订单 ${prior.order_number}` : '创建订单'}</h2>
    <p className="desk-hint">{prior ? `当前 V${prior.version}。保存后产生新版本。` : `为${merchant.legal_name}登记业务指令。`} 建单仅登记业务，资金动作待后续审批。</p>
    <form onSubmit={submit}>
      <fieldset className="desk-fields" disabled={busy || uncertain}>
        <div className="desk-form-grid">
          <Field label="业务方向"><select value={direction} disabled={Boolean(prior)} onChange={e => setDirection(e.target.value as Direction)}>
            {Object.entries(directions).map(([value, title]) => <option key={value} value={value}>{title}</option>)}
          </select></Field>
          <Field label={`来源金额（${source}）`}><input name="amount" type="text" required inputMode="decimal" autoComplete="off" defaultValue={prior?.source_amount} pattern={source !== 'USDT' ? '[0-9]+(\\.[0-9]{1,2})?' : '[0-9]+(\\.[0-9]{1,6})?'} title={source !== 'USDT' ? '法币金额最多保留两位小数' : 'USDT 金额最多保留六位小数'} /></Field>
        </div>
        <Field label="客户原始指令"><textarea name="instruction" rows={4} required defaultValue={prior?.instruction} /></Field>
        <p>本次商户资料 <strong>V{profileVersion}</strong>{prior && prior.profile_version !== profileVersion && `（原订单 V${prior.profile_version}）`} <button type="button" className="text-button" onClick={reloadResources}>刷新可选资料</button></p>
        <div className="desk-form-grid" key={direction}>
          <Field label="收款目标版本"><select name="target" defaultValue={prior?.target_version_id || ''}>
            <option value="">待补充</option>
            {prior?.target_version_id && !targets.some(t => t.id === prior.target_version_id) && <option value={prior.target_version_id}>{prior.pinned_target ? targetCaption(prior.pinned_target) : `原订单目标 · ${prior.target_version_id}`} </option>}
            {targets.map(t => <option key={t.id} value={t.id}>{targetCaption(t)}</option>)}
          </select></Field>
          <Field label="通道资料"><select name="binding" defaultValue={prior?.channel_binding_id || ''}>
            <option value="">待补充</option>
            {prior?.channel_binding_id && !bindings.some(b => b.id === prior.channel_binding_id) && <option value={prior.channel_binding_id}>原订单通道资料 · {prior.channel_binding_id}</option>}
            {bindings.map(b => <option key={b.id} value={b.id}>{bindingCaption(b)}</option>)}
          </select></Field>
        </div>
        <label className="checkbox"><input type="checkbox" checked={includeQuote} onChange={e => setIncludeQuote(e.target.checked)} />录入本次报价</label>
        {includeQuote && <div className="desk-quote" key={direction}>
          <div className="desk-form-grid">
            <Field label={`报价（1 ${source} 兑换的 ${destination}）`}><input name="rate" required inputMode="decimal" defaultValue={prior?.quote?.rate} pattern="[0-9]+(\.[0-9]+)?" /></Field>
            <Field label={`报价到账金额（${destination}）`}><input name="destination_amount" required inputMode="decimal" defaultValue={prior?.quote?.destination_amount} pattern="[0-9]+(\.[0-9]+)?" /></Field>
          </div>
          <Field label="报价有效期" hint="按当前设备的本地时间录入。"><input name="expires" type="datetime-local" step="any" required defaultValue={prior?.quote ? localDateValue(prior.quote.expires_at) : ''} /></Field>
          <Field label="报价依据"><input name="evidence" required defaultValue={prior?.quote?.evidence} /></Field>
        </div>}
      </fieldset>
      <ErrorNotice error={error} />
      {fiatChanged && <p className="notice" role="alert">受理法币已更新为 {resources.fiat_asset}。请返回列表，按新币种重新建单。</p>}
      <div className="form-actions"><button type="button" className="button secondary" onClick={onBack} disabled={busy}>返回</button>
        <button className="button primary" disabled={busy || ((!resources || !fiatAsset || fiatChanged) && !uncertain)}>{busy ? '正在保存…' : uncertain ? '重试原请求' : prior ? '保存新版本' : '保存订单'}</button>
      </div>
    </form>
  </section>
}
