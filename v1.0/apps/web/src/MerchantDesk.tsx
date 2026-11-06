import { useState } from 'react'
import type { Merchant } from './api'
import { useServerRead, type Resources } from './desk-data'
import { ResourceDesk } from './ResourceDesk'
import { OrdersDesk } from './OrdersDesk'
import { ErrorNotice } from './ui'
import './merchant-desk.css'

export function MerchantDesk({ merchant }: { merchant: Merchant }) {
  const [page, setPage] = useState<'orders' | 'resources'>('orders')
  const base = `/merchants/${encodeURIComponent(merchant.id)}`
  const resources = useServerRead<Resources>(`${base}/resources`, merchant.row_version)
  return <section className="desk">
    <nav className="desk-nav" aria-label="商户服务">
      <button type="button" aria-current={page === 'orders' ? 'page' : undefined} onClick={() => setPage('orders')}>业务订单</button>
      <button type="button" aria-current={page === 'resources' ? 'page' : undefined} onClick={() => setPage('resources')}>资料与收款目标</button>
    </nav>
    {resources.error && <div><ErrorNotice error={resources.error} /><button className="text-button" onClick={resources.reload}>重新读取商户资料</button></div>}
    {page === 'orders'
      ? <OrdersDesk merchant={merchant} base={base} resources={resources.data} reloadResources={resources.reload} />
      : resources.data
        ? <ResourceDesk merchant={merchant} base={base} resources={resources.data} onChange={resources.reload} />
        : resources.loading && <p role="status">正在读取资料…</p>}
  </section>
}
