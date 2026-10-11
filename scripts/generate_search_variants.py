#!/usr/bin/env python3
"""Generate a deterministic, licensed character-equivalence search table.

This is search recall, NOT linguistic translation. Never rewrites source names.
Run explicitly to refresh the pinned upstream artifacts; runtime stays offline.
"""

import hashlib
import json
from pathlib import Path
from urllib.request import urlopen

ROOT = Path(__file__).resolve().parents[1]
DEST = ROOT / "services/platform-api/internal/searchtext/data"
BASE = "https://raw.githubusercontent.com/BYVoid/OpenCC/ver.1.1.9/"
EXPECTED = {
    "TSCharacters.txt": "6b5a0a799bea2bb22c001f635eaa3fc2904310f0c08addbff275477a80ecf09a",
    "JPVariants.txt": "db09543db11ffd42a2556070f3d0a8e2c0168726463bd5f8d530f3e54a5afab8",
    "LICENSE": "b534e465949558eec2597b04f5092b5e161236a68dfbfd04d547592ac3964308",
}


def main():
    artifacts = {}
    for name, path in [
        ("TSCharacters.txt", "data/dictionary/TSCharacters.txt"),
        ("JPVariants.txt", "data/dictionary/JPVariants.txt"),
        ("LICENSE", "LICENSE"),
    ]:
        with urlopen(BASE + path, timeout=30) as response:
            raw = response.read(2_000_001)
        if len(raw) > 2_000_000:
            raise ValueError("unexpected upstream size")
        if hashlib.sha256(raw).hexdigest() != EXPECTED[name]:
            raise ValueError("upstream artifact hash changed")
        artifacts[name] = (raw, BASE + path)
    parent = {}
    simplified = set()
    traditional = set()

    def root(c):
        parent.setdefault(c, c)
        if parent[c] != c:
            parent[c] = root(parent[c])
        return parent[c]

    for name in ["TSCharacters.txt", "JPVariants.txt"]:
        for line in artifacts[name][0].decode("utf-8").splitlines():
            if not line or line.startswith("#"):
                continue
            source, values = line.split("\t", 1)
            target = values.split(" ")[0]
            if len(source) != 1 or len(target) != 1:
                raise ValueError("not a character dictionary")
            a, b = root(source), root(target)
            parent[a] = b
            if name == "TSCharacters.txt":
                simplified.add(target)
                traditional.add(source)
    groups = {}
    for c in sorted(parent):
        groups.setdefault(root(c), []).append(c)
    mappings = {}
    for chars in groups.values():
        canonical = min(
            chars, key=lambda c: (not (c in simplified and c not in traditional), c not in simplified, ord(c))
        )
        for c in chars:
            if c != canonical:
                mappings[c] = canonical
    source = "".join(sorted(mappings))
    target = "".join(mappings[c] for c in source)
    assert 1000 < len(source) < 10000 and len(source) == len(target)
    manifest = {
        "version": "opencc-1.1.9-character-equivalence-v1",
        "mode": "first single-character candidate, transitive equivalence; NOT phrase translation",
        "upstream": [
            {"file": name, "url": url, "sha256": hashlib.sha256(raw).hexdigest()}
            for name, (raw, url) in artifacts.items()
        ],
        "mapped_characters": len(source),
    }
    DEST.mkdir(parents=True, exist_ok=True)
    for name, (raw, _) in artifacts.items():
        (DEST / name).write_bytes(raw)
    (DEST / "variants.json").write_text(
        json.dumps({"source": source, "target": target}, ensure_ascii=False) + "\n", encoding="utf-8"
    )
    (DEST / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(manifest))


if __name__ == "__main__":
    main()
