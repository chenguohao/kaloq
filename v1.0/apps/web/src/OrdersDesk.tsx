import { useState } from 'react'
import type { Merchant } from './api'
import { bindingCaption, directions, displayTime, targetCaption, useServerRead, type Order, type OrderSummary, type Resources } from './desk-data'
import { OrderForm } from './OrderForm'
import { ErrorNotice } from './ui'

type View = { name: 'list' | 'create' } | { name: 'detail'; id: string; version?: number } | { name: 'edit'; order: Order }
const statusNames: Record<string, string> = { blocked: '待处理', recorded: '已登记', pending: '待前置环节', planned: '已规划' }

export function OrdersDesk({ merchant, base, resources, reloadResources }: {
  merchant: Merchant; base: string; resources?: Resources; reloadResources: () => void
}) {
  const [view, setView] = useState<View>({ name: 'list' })
  const [offset, setOffset] = useState(0)
  const orders = useServerRead<{ items: OrderSummary[]; next_offset: number | null }>(
    view.name === 'list' ? `${base}/orders?offset=${offset}` : null,
  )
  const detail = useServerRead<{ order: Order }>(view.name === 'detail'
    ? `${base}/orders/${encodeURIComponent(view.id)}${view.version ? `?version=${view.version}` : ''}` : null)
  const toList = () => { orders.reload(); setView({ name: 'list' }) }
  const open = (id: string, version?: number) => setView({ name: 'detail', id, version })
  if (view.name === 'create' || view.name === 'edit') {
    const prior = view.name === 'edit' ? view.order : undefined
    return <OrderForm key={prior?.id || 'new'} merchant={merchant} base={base} resources={resources} reloadResources={reloadResources}
      prior={prior} onBack={prior ? () => open(prior.id) : toList} onSaved={order => open(order.id, order.version)} />
  }
  if (view.name === 'detail') {
    return <div className="desk-stack"><button className="text-button desk-back" onClick={toList}>← 订单列表</button>
      <ErrorNotice error={detail.error} />
      {detail.loading && <p role="status">正在读取订单…</p>}
      {detail.error && <button className="text-button" onClick={detail.reload}>重新读取订单</button>}
      {detail.data && <OrderDetails order={detail.data.order} resources={resources} refresh={detail.reload}
        edit={() => setView({ name: 'edit', order: detail.data!.order })} changeVersion={version => open(view.id, version)} />}
    </div>
  }
  const data = orders.data
  return <section className="panel desk-panel">
    <div className="desk-heading"><h2>业务订单</h2><div className="desk-actions">
      <button className="text-button" onClick={orders.reload} disabled={orders.loading}>刷新订单</button>
      <button className="button primary" disabled={!resources?.fiat_asset} onClick={() => setView({ name: 'create' })}>创建订单</button>
    </div></div>
    <ErrorNotice error={orders.error} />
    {orders.loading && <p role="status">正在读取订单…</p>}
    {data && (data.items.length ? <ul className="desk-orders">{data.items.map(order => <li key={order.id}>
      <div><button className="text-button desk-number" onClick={() => open(order.id)}>{order.order_number}</button>
        <span className="desk-sub">{directions[order.direction]} · V{order.version} · {order.operator_name}</span>
        <span className="desk-sub">{displayTime(order.created_at)}</span></div>
      <div className="desk-amount">{order.source_amount} <small>{order.source_asset}</small></div>
      <div className="desk-list-state"><strong>{statusNames[order.status] || order.status}</strong><span>{order.hold_reason}</span></div>
      <button className="text-button" onClick={() => open(order.id)}>查看详情</button>
    </li>)}</ul> : <p className="desk-empty">{offset ? '该页暂无订单。' : '该商户尚无业务订单。'}</p>)}
    {data && (offset > 0 || data.next_offset !== null) && <div className="desk-pager">
      <button className="text-button" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - 100))}>上一页</button>
      <span>第 {Math.floor(offset / 100) + 1} 页</span>
      <button className="text-button" disabled={data.next_offset === null} onClick={() => data.next_offset !== null && setOffset(data.next_offset)}>下一页</button>
    </div>}
  </section>
}

function OrderDetails({ order, resources, edit, refresh, changeVersion }: {
  order: Order; resources?: Resources; edit: () => void; refresh: () => void; changeVersion: (version: number) => void
}) {
  const { quote } = order
  const currentStage = order.stages.find(stage => stage.status === 'blocked')
  const historic = order.version !== order.current_version
  const target = order.pinned_target || resources?.targets.find(t => t.id === order.target_version_id)
  const binding = order.pinned_binding || resources?.bindings.find(b => b.id === order.channel_binding_id)
  const bindingLabel = binding ? bindingCaption(binding) : order.channel_binding_id || '待补充'
  const profile = order.pinned_profile || resources?.profiles.find(p => p.version === order.profile_version)
  return <>
    <section className="panel desk-panel">
      <div className="desk-heading">
        <div><h2 className="desk-number">{order.order_number}</h2><p className="desk-hint">{directions[order.direction]} · {order.operator_name} · 建立于 {displayTime(order.created_at)}</p></div>
        <div className="desk-actions"><button className="text-button" onClick={refresh}>刷新详情</button>
          <button className="button secondary" disabled={historic} onClick={edit}>修订订单</button></div>
      </div>
      <div className="desk-summary"><div><span>来源金额</span><strong>{order.source_amount} <small>{order.source_asset}</small></strong></div>
        <div><label htmlFor="order-version">订单版本</label><select id="order-version" value={order.version} onChange={e => changeVersion(Number(e.target.value))}>
          {Array.from({ length: order.current_version }, (_, n) => order.current_version - n).map(n => <option key={n} value={n}>V{n} {n === order.current_version ? '（当前）' : '（历史）'}</option>)}
        </select></div></div>
      <div className="desk-blocker">{currentStage && <><h3>{historic ? '该版本的环节' : '当前环节'}</h3><p>{currentStage.label}</p></>}<h3>{historic ? '该版本的卡点' : '当前卡点'}</h3><p>{order.hold_reason}</p>
        <h3>下一步</h3><p>{order.next_action}</p></div>
    </section>
    <section className="panel desk-panel"><h2>订单进度</h2>
      <ol className="desk-pipeline">{order.stages.map(stage => <li key={stage.code} data-state={stage.status}>
        <div><strong>{stage.label}</strong><span className="desk-state">{statusNames[stage.status] || stage.status}</span></div>
        {stage.reason && <p>{stage.reason}</p>}
      </li>)}</ol>
    </section>
    <section className="panel desk-panel"><h2>指令与资料版本</h2>
      <dl className="desk-dl">
        <div><dt>登记人 / 时间</dt><dd>{order.recorded_by_name} · {displayTime(order.version_created_at)}</dd></div>
        <div><dt>商户资料</dt><dd>V{order.profile_version}{profile && ` · ${profile.legal_name}`}</dd></div>
        <div><dt>收款目标版本</dt><dd>{target ? targetCaption(target) : order.target_version_id || '待补充'}</dd></div>
        <div><dt>通道资料</dt><dd>{bindingLabel}</dd></div>
        <div className="desk-wide"><dt>客户原始指令</dt><dd className="desk-instruction">{order.instruction}</dd></div>
      </dl>
    </section>
    <section className="panel desk-panel"><h2>{quote ? `报价 V${quote.version}` : '报价'}</h2>
      {quote ? <dl className="desk-dl"><div><dt>报价汇率</dt><dd>1 {order.source_asset} = {quote.rate} {order.destination_asset}</dd></div>
        <div><dt>报价到账金额</dt><dd>{quote.destination_amount} {order.destination_asset}</dd></div>
        <div><dt>有效期</dt><dd>{displayTime(quote.expires_at)}{(quote.expired || Date.parse(quote.expires_at) <= Date.now()) && '（已过期）'}</dd></div>
        <div><dt>报价依据</dt><dd>{quote.evidence}</dd></div></dl> : <p className="desk-empty">本版本尚未录入报价。</p>}
    </section>
    <section className="panel desk-panel"><h2>计划执行子单</h2>
      <ul className="desk-rows">{order.steps.map(step => <li key={step.id}>
        <div><strong>{step.label}</strong><span>{statusNames[step.status] || step.status}</span></div>
        <div><span>通道实例 · {step.instance_id || '待绑定'}</span><span>账户绑定 · {step.binding_id || '待绑定'}</span></div>
      </li>)}</ul>
    </section>
  </>
}
