// Synthetic accounts on an isolated database only; never logs passwords, TOTP or recovery codes.
import { createHmac, randomBytes } from 'node:crypto';
import { readFile, mkdir, writeFile } from 'node:fs/promises';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.CHECK_BASE || 'http://localhost:4201/';
const creds = JSON.parse(await readFile(process.env.CHECK_CREDENTIALS, 'utf8'));
const output = process.env.CHECK_OUTPUT || '/tmp/kaloq-d2/browser-evidence';
await mkdir(output, { recursive: true });
let stage = 'launch'; const passed = []; const errors = [];
const browser = await chromium.launch({ executablePath: '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome', headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
const page = await context.newPage();
page.on('pageerror', () => errors.push('browser page error'));
const steps = new Map();
function totp(secret, step) {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'; let bits = '';
  for (const c of secret.replace(/=+$/, '')) bits += alphabet.indexOf(c).toString(2).padStart(5, '0');
  const bytes=[];for(let i=0;i+8<=bits.length;i+=8)bytes.push(parseInt(bits.slice(i,i+8),2));
  const count=Buffer.alloc(8);count.writeBigUInt64BE(BigInt(step));
  const mac=createHmac('sha1',Buffer.from(bytes)).update(count).digest();const offset=mac[19]&15;
  return ((mac.readUInt32BE(offset)&0x7fffffff)%1000000).toString().padStart(6,'0');
}
async function fresh(secret) {
  let step=Math.floor(Date.now()/30000);
  while(step<= (steps.get(secret) ?? -1)){await new Promise(r=>setTimeout(r,500));step=Math.floor(Date.now()/30000)}
  steps.set(secret,step);return totp(secret,step);
}
async function enroll(login,password) {
  await page.getByLabel('员工账号',{exact:true}).fill(login);
  await page.getByLabel('密码',{exact:true}).fill(password);
  await page.getByRole('button',{name:'继续验证',exact:true}).click();
  await page.getByRole('heading',{name:'绑定身份认证器'}).waitFor();
  const secret=await page.locator('code.secret').textContent();
  await page.getByLabel('六位动态码').fill(await fresh(secret));
  await page.getByRole('button',{name:'验证并继续',exact:true}).click();
  await page.getByRole('heading',{name:'保存您的恢复码'}).waitFor();
  const codes=await page.locator('.recovery-codes code').allTextContents();
  await page.getByRole('checkbox').check();await page.getByRole('button',{name:'进入工作台',exact:true}).click();
  await page.getByRole('navigation',{name:'主导航'}).waitFor();
  return {secret,codes};
}
try {
  stage='login page';await page.goto(base);await page.getByRole('heading',{name:'登录工作台'}).waitFor();
  await page.screenshot({path:output+'/login-desktop.png',fullPage:true});
  stage='root enrollment';const root=await enroll(creds.login,creds.password);passed.push(stage);
  stage='staff creation';await page.getByRole('button',{name:'添加员工',exact:true}).click();
  const login='browser-'+randomBytes(4).toString('hex'),password=randomBytes(24).toString('base64url');
  await page.getByLabel('员工姓名',{exact:true}).fill('浏览器验收运营');
  await page.getByLabel('登录账号',{exact:true}).fill(login);
  await page.getByLabel('初始密码',{exact:false}).fill(password);
  await page.getByRole('checkbox',{name:'运营',exact:true}).check();
  await page.getByLabel('六位动态码').fill(await fresh(root.secret));
  await page.getByRole('button',{name:'确认保存',exact:true}).click();
  await page.getByRole('cell').filter({hasText:login}).waitFor();passed.push(stage);
  await page.getByLabel('退出登录',{exact:true}).click();
  stage='operator enrollment';const operator=await enroll(login,password);passed.push(stage);
  stage='merchant creation';await page.getByRole('button',{name:'创建商户',exact:true}).click();
  await page.getByLabel('商户主体名称',{exact:true}).fill('验收样例商户');
  await page.getByLabel('联系人姓名',{exact:true}).fill('样例联系人');
  await page.getByLabel('联系邮箱',{exact:true}).fill('sample@example.invalid');
  await page.getByRole('button',{name:'创建档案',exact:true}).click();
  await page.getByRole('heading',{name:'验收样例商户',exact:true}).waitFor();passed.push(stage);
  await page.screenshot({path:output+'/merchant-desktop.png',fullPage:true});
  stage='merchant update';await page.getByRole('button',{name:'编辑资料',exact:true}).click();
  await page.getByLabel('联系人姓名',{exact:true}).fill('更新后的联系人');
  await page.getByRole('button',{name:'保存修改',exact:true}).click();
  await page.getByText('更新后的联系人',{exact:true}).waitFor();passed.push(stage);
  stage='mobile layout';await page.setViewportSize({width:390,height:844});
  if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1))throw Error('overflow');
  await page.screenshot({path:output+'/merchant-mobile.png',fullPage:true});passed.push(stage);
  await page.setViewportSize({width:1440,height:1000});
  stage='virtual WebAuthn registration';
  const cdp=await context.newCDPSession(page);await cdp.send('WebAuthn.enable');
  await cdp.send('WebAuthn.addVirtualAuthenticator',{options:{protocol:'ctap2',transport:'internal',hasResidentKey:true,hasUserVerification:true,isUserVerified:true,automaticPresenceSimulation:true}});
  await page.getByRole('button',{name:'个人安全',exact:true}).click();
  await page.getByRole('button',{name:'登记新凭证',exact:true}).click();
  await page.getByLabel('凭证名称',{exact:false}).fill('自动化虚拟凭证');
  await page.getByLabel('六位动态码').fill(await fresh(operator.secret));
  await page.getByRole('button',{name:'继续登记',exact:true}).click();
  await page.getByText('个人凭证已登记，可继续验证其可用性。',{exact:true}).waitFor();passed.push(stage);
  stage='virtual WebAuthn assertion';await page.getByRole('button',{name:'验证凭证可用性',exact:true}).click();
  await page.getByRole('status').filter({hasText:'凭证验证通过'}).waitFor();passed.push(stage);
  stage='recovery and old credential invalidation';await page.getByLabel('退出登录',{exact:true}).click();
  await page.getByLabel('员工账号',{exact:true}).fill(login);await page.getByLabel('密码',{exact:true}).fill(password);
  await page.getByRole('button',{name:'继续验证',exact:true}).click();
  await page.getByRole('button',{name:'无法使用认证器？使用恢复码',exact:true}).click();
  await page.getByLabel('一次性恢复码',{exact:true}).fill(operator.codes[0]);
  await page.getByRole('button',{name:'验证并重新绑定',exact:true}).click();
  await page.getByRole('heading',{name:'绑定身份认证器'}).waitFor();
  const newSecret=await page.locator('code.secret').textContent();
  await page.getByLabel('六位动态码').fill(await fresh(newSecret));await page.getByRole('button',{name:'验证并继续',exact:true}).click();
  await page.getByRole('checkbox').check();await page.getByRole('button',{name:'进入工作台',exact:true}).click();
  await page.getByRole('button',{name:'个人安全',exact:true}).click();await page.getByText(/^(已撤销|已失效)$/).waitFor();passed.push(stage);
  if(errors.length)throw Error('browser errors');
  await writeFile(output+'/result.json',JSON.stringify({passed,errors,hardware_verified:false},null,2));
  console.log(JSON.stringify({passed,errors,hardware_verified:false}));
} catch(e) {console.error(JSON.stringify({failed_stage:stage,passed,error_type:e.name}));process.exitCode=1;}
finally {await browser.close()}
