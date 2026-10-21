#!/usr/bin/env python3
"""Guard book scope and exclude code/appendices from the prose threshold."""
from pathlib import Path
import json
import re
root = Path(__file__).resolve().parents[1]
chapters = sorted((root / 'book/chapters').glob('*.md'))
body = [p for p in chapters if re.match(r'(0[1-9]|1[0-8])-', p.name)]
assert len(body) == 18, len(body)
counts = {}
for path in body:
    text = path.read_text()
    text = re.sub(r'```.*?```', '', text, flags=re.S)
    count = len(re.findall(r'[\u4e00-\u9fff]', text))
    assert count >= 1200, (path.name, count)
    assert '练习' in text and '本章实测证据' in text, path.name
    counts[path.stem] = count
assert sum(counts.values()) >= 25000, sum(counts.values())
assert len([p for p in chapters if p.name.startswith(('20-', '21-', '22-'))]) == 3
print(json.dumps({'body_chapters': 18, 'body_chinese_characters_excluding_code': sum(counts.values()), 'chapter_counts': counts}, ensure_ascii=False))
