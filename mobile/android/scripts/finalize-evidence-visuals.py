#!/usr/bin/env python3
"""Record manual review, without changing PNG pixels or manufacturing W/V evidence."""
import hashlib,json,pathlib,sys
out=pathlib.Path(sys.argv[1]).resolve()
if (out/'png').is_dir(): out=out/'png'
assert (out.parent/'junit-summary.json').is_file(), 'Must finalize an actual enabled evidence run'
s=json.loads((out.parent/'junit-summary.json').read_text());assert s['failures']==0 and s['tests']==21 and s['skipped']==5
for p in sorted(out.glob('*.json')):
 d=json.loads(p.read_text())
 # capture dialog-window when present still requires separate pixel observation;
 # currently the enabled run has exactly 18 app-content PNGs.
 assert d['bitmapPx']==d['actualAppRootPx']=='1170x2364' and d['captureScope']=='app-content-only' and not d['productionDialogWindow']
 assert hashlib.sha256((out/(d['case']+'.png')).read_bytes()).hexdigest()==d['checksumSha256']
 d['networkFixture']='loopback, exact method/path/query; see mock-routes-<test>.txt in this directory'
 d['visualVerdict']='READ_CURRENT_BATCH_CONTENT_RECT_VERIFIED_NOT_V_PASS'
 noted={
 'auth-step1-login':'Read with ref2/3: light palette, account, social buttons, footer intact; system chrome intentionally omitted.',
 'auth-step2-password':'Read with ref2/10: header, masked invalid account, secure field, forgot link, margins intact.',
 }
 d['visualNote']=noted.get(d['case'], 'Manually Read this batch: production light-mode content/state and margins present, no top/bottom clip. No ref-matched V pass or device claim.')
 d['deviceVisualVerdict']='NOT_RUN'
 p.write_text(json.dumps(d,indent=2,ensure_ascii=False)+'\n')
print('Finalized manual Read verdicts on',len(list(out.glob('*.json'))),'unchanged production-content PNGs')
