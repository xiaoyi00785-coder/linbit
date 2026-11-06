#!/usr/bin/env python3
"""Record manual review, without changing PNG pixels or manufacturing W/V evidence."""
import hashlib,json,pathlib,sys
out=pathlib.Path(sys.argv[1]).resolve()
if (out/'png').is_dir(): out=out/'png'
assert (out.parent/'manual-read.json').is_file(), 'Read every current PNG first; record the manual proof separately, do not manufacture it in this script'
assert (out.parent/'junit-summary.json').is_file(), 'Must finalize an actual enabled evidence run'
s=json.loads((out.parent/'junit-summary.json').read_text());assert s['failures']==0 and s['tests']==44
for p in sorted(out.glob('*.json')):
 d=json.loads(p.read_text())
 # capture dialog-window when present still requires separate pixel observation;
 # current enabled run: app/dialog PNGs are typed independently app-content PNGs have actual width/height/hash.
 window=d['productionDialogWindow']
 assert (window and d['captureScope']=='dialog-content-only') or (not window and d['captureScope']=='app-content-only' and d['bitmapPx']==d['actualAppRootPx'])
 if not window:
  assert (d['bitmapPx'],d['qualifier'].split(';')[0]) in {
  ('960x2364','w320dp'),('1080x2364','w360dp'),('1170x2364','w390dp'),('1236x2577','w412dp')
 }, 'Actual pixels must be px-per-dp * real host content width/height'
 assert hashlib.sha256((out/(d['case']+'.png')).read_bytes()).hexdigest()==d['checksumSha256']
 d['inspectedState']='production loading at capture, empty grid mock; no empty-state visual pass' if d['case']=='asset-strategy-empty' else 'manifest state Read'
 d['networkFixture']='loopback, exact method/path/query; see mock-routes-<test>.txt in this directory'
 d['visualVerdict']='READ_CURRENT_BATCH_CONTENT_RECT_VERIFIED_NOT_V_PASS'
 noted={
 'auth-step1-login':'Read with ref2/3: light palette, account, social buttons, footer intact; system chrome intentionally omitted.',
 'auth-step2-password':'Read with ref2/10: header, masked invalid account, secure field, forgot link, margins intact.',
 }
 d['visualNote']=noted.get(d['case'], 'Read round2 final4; recorded target qualifier in manifest. No ref-complete V pass; no device claim.')
 d['deviceVisualVerdict']='NOT_RUN'
 d['captureMethod']='independent Dialog Canvas; NOT PixelCopy/device' if window else 'Activity content Canvas; not device screen'
 p.write_text(json.dumps(d,indent=2,ensure_ascii=False)+'\n')
print('Finalized manual Read verdicts on',len(list(out.glob('*.json'))),'unchanged production-content PNGs')

manual=json.loads((out.parent/'manual-read.json').read_text())
assert manual['readCount']==len(list(out.glob('*.png')))==s['png']
for name in manual['files']: assert (out/name).is_file()
