#!/usr/bin/env python3
"""One-time initial-owner smoke test. Leaves owner requiring fresh TOTP enrollment.
Credentials are loaded from a local ignored file; response secrets are never logged.
Run only immediately after bootstrapping an unused formal sandbox owner.
"""
import base64, hashlib, hmac, http.cookiejar, json, struct, sys, time
from pathlib import Path
from urllib.request import Request, build_opener, HTTPCookieProcessor
from urllib.error import HTTPError
from urllib.parse import urlsplit

credentials = json.loads(Path(sys.argv[1]).read_text())
base = credentials['url'].rstrip('/') + '/api/v1'
u = urlsplit(base); origin = f'{u.scheme}://{u.netloc}'
jar = http.cookiejar.CookieJar(); client = build_opener(HTTPCookieProcessor(jar))
checks = []

def request(path, body=None, csrf=None):
    headers={'Origin': origin, 'Content-Type':'application/json', 'User-Agent':'Kaloq-Day2-Check/1.0'}
    if csrf: headers['X-CSRF-Token']=csrf
    r=Request(base+path, data=None if body is None else json.dumps(body).encode(),headers=headers)
    try:
        response=client.open(r,timeout=20)
    except HTTPError as e: response=e
    return response.status, json.loads(response.read())

def check(name, actual, expected):
    if actual!=expected:
        print(json.dumps({'failed':name,'status':actual}));raise SystemExit(1)
    checks.append({'check':name,'status':actual})

def code(secret):
    key=base64.b32decode(secret+'='*((-len(secret))%8))
    d=hmac.new(key,struct.pack('>Q',int(time.time())//30),hashlib.sha1).digest();o=d[-1]&15
    return str((struct.unpack('>I',d[o:o+4])[0]&0x7fffffff)%1000000).zfill(6)

check('unauthenticated me',request('/me')[0],401)
s,r=request('/auth/login',{'login_name':credentials['login'],'password':credentials['password']});check('password login',s,200)
if r.get('stage')!='enroll':raise SystemExit('Owner is already enrolled; refusing to change an existing factor')
check('challenge cannot access staff',request('/staff')[0],401)
s,r=request('/auth/enrollment');check('enrollment challenge',s,200);secret=r['secret'];step=int(time.time())//30
s,r=request('/auth/enrollment',{'code':code(secret)});check('TOTP enrollment',s,200);recovery=r['recovery_codes'][0]
s,r=request('/me');check('full authenticated session',s,200);csrf=r['csrf_token']
check('staff list with runtime role',request('/staff')[0],200)
check('maintenance has no merchant rights',request('/merchants')[0],403)
check('passkey list with runtime role',request('/security/passkeys')[0],200)
while int(time.time())//30<=step:time.sleep(.5)
check('passkey begin with runtime role',request('/security/passkeys/register/begin',{'code':code(secret),'label':'首次部署验证'},csrf)[0],200)
check('logout',request('/auth/logout',{},csrf)[0],200)
s,r=request('/auth/login',{'login_name':credentials['login'],'password':credentials['password']});check('return login',s,200)
check('recovery resets to enrollment',request('/auth/recover',{'recovery_code':recovery})[0],200)
check('recovery has no full session',request('/me')[0],401)
check('logout restricted challenge',request('/auth/logout',{})[0],200)
result={'checks':checks,'owner_state':'requires_first_factor_enrollment','secrets_logged':False}
print(json.dumps(result,ensure_ascii=False,indent=2))
