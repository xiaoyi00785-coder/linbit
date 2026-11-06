#!/usr/bin/env bash
# Local-only, isolated mock evidence. Does not deploy, create accounts, or submit money APIs.
set -euo pipefail
cd "$(dirname "$0")/.."
OUT="${PRODUCTION_EVIDENCE_OUT:-}"
if [[ -z "$OUT" || "$OUT" != /* ]]; then
  printf 'Set PRODUCTION_EVIDENCE_OUT to an absolute, external evidence directory\n' >&2
  exit 2
fi
mkdir -p "$OUT/png" "$OUT/junit"
find "$OUT/png" -maxdepth 1 -type f \( -name '*.png' -o -name '*.json' -o -name '*.properties' -o -name 'mock-routes-*.txt' \) -delete
find "$OUT/junit" -type f -delete
exec > >(tee "$OUT/gradle.log") 2>&1
rm -rf app/build/test-results/testDebugUnitTest app/build/reports/tests/testDebugUnitTest
# Preserve standalone JUnit evidence from default skipped runs; only the declared scope may run.
set +e
./gradlew :app:testDebugUnitTest \
  --tests 'click.cowa.linbit.feature.profile.SecurityFeatureTest' \
  --tests 'click.cowa.linbit.ui.evidence.ProductionSecurityEvidenceTest' \
  --tests 'click.cowa.linbit.ui.evidence.ProductionAuthEvidenceTest' \
  --tests 'click.cowa.linbit.ui.evidence.ProductionAssetEvidenceTest' \
  --tests 'click.cowa.linbit.ui.evidence.ProductionConvertConfirmationTest' \
  -Doffscreen.shots=1 -Dproduction.evidence=1 -Dproduction.evidence.dir="$OUT/png" \
  --no-build-cache
status=$?
set -e
if [[ -d app/build/test-results/testDebugUnitTest ]]; then
  cp -a app/build/test-results/testDebugUnitTest/. "$OUT/junit/"
else
  printf 'No XML emitted: compilation or runner failed before test execution\n' > "$OUT/junit/NO_JUNIT_REPORT.txt"
fi
printf 'gitSha=%s\nverifiedWorkingTree=%s\nenabledTestExit=%s\nscope=SecurityFeatureTest(6),ProductionAuthEvidenceTest(4),ProductionAssetEvidenceTest(8),ProductionConvertConfirmationTest(1),ProductionSecurityEvidenceTest(2)\n' "$(git rev-parse HEAD)" "$(git diff --quiet && echo clean || echo local-uncommitted-validation)" "$status" > "$OUT/run.properties"
python3 - "$OUT" <<'PY_SUMMARY'
import json, pathlib, sys, xml.etree.ElementTree as E
out=pathlib.Path(sys.argv[1]);total=failed=skipped=0;details=[]
for p in sorted((out/'junit').glob('TEST-*.xml')):
 r=E.parse(p).getroot(); a=r.attrib; row={'file':p.name,'tests':int(a['tests']),'failures':int(a.get('failures','0'))+int(a.get('errors','0')),'skipped':int(a.get('skipped','0'))};details.append(row);total+=row['tests'];failed+=row['failures'];skipped+=row['skipped']
summary={'tests':total,'passed':total-failed-skipped,'failures':failed,'skipped':skipped,'png':len(list((out/'png').glob('*.png'))),'classes':details}
(out/'junit-summary.json').write_text(json.dumps(summary,indent=2))
print('Independent enabled JUnit summary:',json.dumps(summary))
if failed or total != 21 or skipped != 5: print('This is NOT a complete enabled five-skip run.')
PY_SUMMARY
if [[ "$status" -ne 0 ]]; then exit "$status"; fi
printf 'instrumentation=compile-only;NOT_RUN_WITHOUT_DEVICE\nnetwork=loopback explicit mock;production HTTP disabled by test Application\n' >> "$OUT/run.properties"
./gradlew :app:compileDebugAndroidTestKotlin
printf 'JUnit copied separately: %s/junit\nPNG and per-shot manifests: %s/png\n' "$OUT" "$OUT"
