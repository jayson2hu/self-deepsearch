#!/usr/bin/env python3
"""Read-only live PostgreSQL search verification; synthetic cases are VALUES, never rows."""
import json
from datetime import datetime, timezone
from pathlib import Path
import collector_integration as tool


def main():
    report = {'checked_at': datetime.now(timezone.utc).isoformat(), 'status': 'running', 'cases': []}
    people = [
        {'name': '公开艺名甲', 'aliases': ['曾用艺名紫苑', 'Stage Alpha']},
        {'name': '另一位人物', 'aliases': ['Stage Alpha']},
        {'name': 'No alias'}, {'name': 'Null alias', 'aliases': None},
        {'name': 'Malformed alias', 'aliases': {'name': '不得命中对象值'}},
        {'name': 'Typed aliases', 'aliases': [None, 123, {'x':'不得命中对象值'}, 'Literal%_Name']},
    ]
    try:
        for query, expected in [('艺名甲',1),('曾用艺名紫苑',1),('名紫苑',1),('stage alpha',2),(' Stage Alpha ',2),('%_',1),('不得命中对象值',0),("' OR true --",0),('',6)]:
            sql = f"""WITH sample AS (SELECT value AS payload, 'source-key'::text external_id
              FROM jsonb_array_elements({tool.literal(json.dumps(people,ensure_ascii=False))}::jsonb))
              SELECT json_build_object('count',count(*)) FROM sample WHERE {tool.performer_search_predicate(query)};"""
            result = tool.postgres(sql)
            assert result['count'] == expected, (query,result)
            report['cases'].append({'query':query,'hits':result['count'],'mode':'read-only VALUES'})
        for query, expected in [('三上悠亜',1),('三上',4),('AIKA',5),('aika',5)]:
            result=tool.read_people(query=query)
            assert result['total'] == expected, (query,result['total'])
            if query.lower() == 'aika':
                assert any(item['name'] == 'AIKA' for item in result['items'])
            report['cases'].append({'query':query,'hits':result['total'],'mode':'real staging'})
        report['counts']=tool.read_people()['counts']
        assert report['counts']=={'performers':4196,'avatars':130,'without_avatar':4066}
        report['status']='passed'
    except Exception:
        report['status']='failed'
        raise
    finally:
        output=Path('docs/evidence/stage-name-search-2026-09-19');output.mkdir(parents=True,exist_ok=True)
        (output/'database.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
        print(json.dumps(report,ensure_ascii=False))


if __name__ == '__main__':
    main()
