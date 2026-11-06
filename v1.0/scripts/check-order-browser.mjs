// Local rehearsal only. CHECK_FIXTURE contains a synthetic operator's existing
// session token and the D2 merchant ID. Run after linking the fixture's VA.
import assert from 'node:assert/strict'
import { readFile, writeFile, mkdir } from 'node:fs/promises'
import { join } from 'node:path'
import { randomUUID } from 'node:crypto'

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const origin = new URL(process.env.CHECK_BASE || 'http://localhost:4233')
assert(['localhost', '127.0.0.1'].includes(origin.hostname))
const fixture = JSON.parse(await readFile(process.env.CHECK_FIXTURE, 'utf8'))
const output = process.env.CHECK_OUTPUT
await mkdir(output, { recursive: true })
const browser = await chromium.launch({ executablePath: process.env.CHROME_PATH, headless: true })
const context = await browser.newContext({ viewport: { width: 1280, height: 900 } })
await context.addCookies([{ name: 'kaloq_v1_session', value: fixture.token, url: origin.origin }])
const page = await context.newPage()
page.setDefaultTimeout(10000)
const button = name => page.getByRole('button', { name, exact: true })
const suffix = randomUUID().slice(0, 8)
const targetName = '验收目标 ' + suffix
const amount = '80.28'
const notes = ['受理指令 ' + suffix, '修订指令 ' + suffix]
const passed = []
const errors = []
let stage = 'Day2 运营会话打开 Day3 商户'
page.on('pageerror', () => errors.push('pageerror'))
try {
  await page.goto(origin.href)
  await page.getByRole('row').filter({ hasText: fixture.merchant_code }).getByRole('button', { name: '进入工作台' }).click()
  await button('资料与收款目标').click()
  await page.getByText('Demo VA 记录', { exact: true }).waitFor()
  passed.push(stage)

  stage = '登记商户链上目标'
  await button('新增收款目标').click()
  let modal = page.getByRole('dialog')
  await modal.getByLabel('名称', { exact: true }).fill(targetName)
  await modal.getByLabel('目标类型').selectOption('chain')
  await modal.getByLabel('网络', { exact: true }).fill('QA-NET')
  await modal.getByLabel('完整链上地址', { exact: true }).fill('example-address-' + suffix)
  await modal.getByLabel('证据引用', { exact: true }).fill('QA-' + suffix)
  await modal.getByRole('button', { name: '保存记录', exact: true }).click()
  await page.getByText(targetName, { exact: true }).waitFor()
  passed.push(stage)

  stage = '登记 HKD 转 USDT 业务单；丢失响应后重试原请求'
  await button('业务订单').click()
  await button('创建订单').click()
  await page.getByLabel('来源金额（HKD）').fill(amount)
  await page.getByLabel('客户原始指令').fill(notes[0])
  const target = page.getByLabel('收款目标版本')
  const targetID = await target.locator('option').filter({ hasText: targetName }).getAttribute('value')
  await target.selectOption(targetID)
  const binding = page.locator('select[name=binding]')
  const vaID = await binding.locator('option').filter({ hasText: 'Demo VA 记录' }).first().getAttribute('value')
  await binding.selectOption(vaID)
  await page.getByLabel('录入本次报价').check()
  await page.locator('input[name=rate]').fill('0.125')
  await page.locator('input[name=destination_amount]').fill('10.035')
  await page.locator('input[name=expires]').fill('2027-01-01T10:00')
  await page.getByLabel('报价依据').fill('虚构客户确认记录')
  let firstOrder
  await page.route('**/orders', async route => {
    const response = await route.fetch()
    assert.equal(response.status(), 201)
    firstOrder = (await response.json()).order
    await route.abort('failed')
  }, { times: 1 })
  await button('保存订单').click()
  await button('重试原请求').click()
  await page.getByRole('heading', { name: '订单进度' }).waitFor()
  await page.getByText(firstOrder.order_number, {exact:true}).waitFor()
  assert.equal(firstOrder.source_asset, 'HKD')
  assert.equal(firstOrder.source_amount, amount)
  assert.equal(firstOrder.channel_binding_id, vaID)
  assert.equal(firstOrder.target_version_id, targetID)
  assert(firstOrder.steps.some(step => step.binding_id === vaID && step.instance_id))
  assert(firstOrder.steps.every(step => step.status === 'planned'))
  passed.push(stage)

  stage = '修订与回看历史指令'
  await button('修订订单').click()
  await page.getByLabel('客户原始指令').fill(notes[1])
  await button('保存新版本').click()
  await page.getByText(notes[1], {exact:true}).waitFor()
  await page.getByLabel('订单版本').selectOption('1')
  await page.getByText(notes[0], {exact:true}).waitFor()
  assert(await button('修订订单').isDisabled())
  await page.getByLabel('订单版本').selectOption('2')
  await page.getByText(notes[1], {exact:true}).waitFor()
  passed.push(stage)

  stage = '桌面与手机订单进度'
  await page.getByRole('heading',{name:'当前卡点'}).scrollIntoViewIfNeeded()
  await page.screenshot({path:join(output,'order-desktop.png')})
  await page.setViewportSize({width:390,height:844})
  await page.getByRole('heading',{name:'当前卡点'}).scrollIntoViewIfNeeded()
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1))
  await page.screenshot({path:join(output,'order-mobile.png')})
  passed.push(stage)

  stage = '反向订单与资料缺失卡点'
  await button('← 订单列表').click()
  await button('创建订单').click()
  await page.getByLabel('业务方向').selectOption('crypto_to_fiat')
  await page.getByLabel('来源金额（USDT）').fill('10.123456')
  await page.getByLabel('客户原始指令').fill('虚构反向业务单 ' + suffix)
  await button('保存订单').click()
  await page.getByRole('heading', { name: '当前卡点' }).waitFor()
  await page.getByText('缺少商户收款目标版本',{exact:true}).first().waitFor()
  passed.push(stage)
  assert.equal(errors.length,0)
  await writeFile(join(output,'browser.json'),JSON.stringify({passed,page_errors:errors.length,fixture:'isolated PostgreSQL and synthetic data'},null,2)+'\n')
  console.log(JSON.stringify({passed}))
} catch(e) {
  console.error(JSON.stringify({failed_stage:stage,passed,error:e.name,reason:String(e.message).split('\n').slice(0,3),alerts:await page.getByRole('alert').allTextContents()}))
  process.exitCode=1
} finally {
  await browser.close()
}
