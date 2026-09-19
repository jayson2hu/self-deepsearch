#!/usr/bin/env python3
"""Live read-only equivalence checks against unchanged collector staging."""
import json
import time
from datetime import datetime,timezone
from pathlib import Path
import collector_integration as tool

report={'checked_at':datetime.now(timezone.utc).isoformat(),'status':'running','cases':[]}
try:
    for variants in [('三上悠亜','三上悠亞','三上悠亚'),('橋本','桥本'),('AIKA','aika','ＡＩＫＡ')]:
        expected=None
        for query in variants:
            start=time.monotonic();data=tool.read_people(query=query);elapsed=round((time.monotonic()-start)*1000)
            ids=sorted(x['performer_id'] for x in data['items'])
            assert data['total']>0 and data['total']<=24
            if expected is None:expected=(data['total'],ids)
            assert expected==(data['total'],ids),(query,data['total'])
            report['cases'].append({'query':query,'hits':data['total'],'elapsed_ms':elapsed})
    report['counts']=tool.read_people()['counts']
    assert report['counts']=={'performers':4196,'avatars':130,'without_avatar':4066}
    assert tool.read_people(query='深田咏美')['total']==0,'must not manufacture translated names'
    report['status']='passed'
except Exception:
    report['status']='failed';raise
finally:
    out=Path('docs/evidence/followup-2026-09-19');out.mkdir(parents=True,exist_ok=True)
    (out/'search-variants-database.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
    print(json.dumps(report,ensure_ascii=False))
