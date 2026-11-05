// Focus-triggered UI invalidation check, using controlled HTTP fixtures (no credentials).
import {writeFile} from 'node:fs/promises';
const {chromium}=await import(process.env.PLAYWRIGHT_MODULE||'playwright');
const browser=await chromium.launch({executablePath:'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',headless:true});
try {
const page=await browser.newPage();let expired=false;
await page.route('**/api/v1/me',r=>r.fulfill({status:expired?401:200,contentType:'application/json',body:JSON.stringify(expired?{error:{code:'unauthenticated',message:'expired'}}:{user:{id:'fixture',login_name:'fixture',display_name:'测试员工',roles:['operator'],status:'active'},csrf_token:'fixture-only'})}));
await page.route('**/api/v1/merchants**',r=>r.fulfill({contentType:'application/json',body:JSON.stringify({items:[]})}));
await page.goto('http://localhost:4201/');await page.getByRole('button',{name:'创建商户',exact:true}).waitFor();
expired=true;await page.evaluate(()=>window.dispatchEvent(new Event('focus')));
await page.getByRole('heading',{name:'登录工作台'}).waitFor();
if(await page.getByRole('button',{name:'创建商户',exact:true}).count())throw Error('protected view retained');
const result={check:'focus revalidation clears expired staff UI',result:'passed',method:'controlled HTTP fixtures'};
await writeFile(process.env.CHECK_OUTPUT||'/tmp/kaloq-d2/session-expiry.json',JSON.stringify(result,null,2));console.log(JSON.stringify(result));
} finally {await browser.close()}
