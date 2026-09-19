import assert from 'node:assert/strict';
import {randomBytes} from 'node:crypto';
import {readFileSync,writeFileSync} from 'node:fs';
import {request} from '@playwright/test';
import {mailpitMessages,messageID,nestedStrings,extractInvitationCode} from './release_a_real_browser_e2e.mjs';

assert.equal(process.env.INTEGRATION_TEST_CONFIRM,'self-deepsearch-review-staging-only');
const display='http://127.0.0.1:13000',preview='http://127.0.0.1:13004',mailpit='http://127.0.0.1:18025';
const user=await request.newContext({extraHTTPHeaders:{Origin:display}}),mail=await request.newContext();
const email=`integration-denied-${Date.now()}@example.test`,password=randomBytes(32).toString('base64url');
const report={status:'running',checked_at:new Date().toISOString(),checks:{},synthetic_account_created:0};
try {
  const challenge=await user.post(display+'/api/v1/auth/signup/code',{data:{email,turnstile_token:'test-pass'}});
  assert.equal(challenge.status(),202);
  let code;
  for(let attempt=0;attempt<30&&!code;attempt++) {
    const messages=mailpitMessages(await (await mail.get(mailpit+'/api/v1/messages')).json());
    const match=messages.find(m=>nestedStrings(m).some(text=>text.includes(email)));
    if(match)code=extractInvitationCode(await (await mail.get(mailpit+'/api/v1/message/'+messageID(match))).json());
    if(!code)await new Promise(resolve=>setTimeout(resolve,500));
  }
  assert.ok(code,'local Mailpit verification code not delivered');
  const signup=await user.post(display+'/api/v1/auth/signup/verify',{data:{email,password,code}});
  assert.equal(signup.status(),201);assert.equal((await signup.json()).user.role,'user');
  report.synthetic_account_created=1;report.synthetic_email=email;
  assert.equal((await (await user.get(display+'/api/v1/me')).json()).user.role,'user');
  const media=JSON.parse(readFileSync('../self-deepsearch-collector/runtime/jable-archive/20260919-final/performer_media_candidates.jsonl','utf8').trim().split('\n')[0]);
  for(const path of ['/','/api/performers',`/media/${media.media_candidate_id}.jpg`,'/preview.js'])assert.equal((await user.get(preview+path)).status(),401);
  report.checks.real_registered_user_denied_all_preview_surfaces=true;
  assert.equal((await user.post(display+'/api/v1/auth/logout')).status(),204);
  assert.equal((await user.get(display+'/api/v1/me')).status(),401);
  report.checks.test_session_logged_out=true;
  report.status='passed';
} catch(error) {
  report.status='failed';report.error=String(error.message).slice(0,500);process.exitCode=1;
} finally {
  await user.dispose();await mail.dispose();
  writeFileSync('docs/evidence/collector-integration-2026-09-19/auth-report.json',JSON.stringify(report,null,2)+'\n');
  console.log(JSON.stringify(report));
}
