"""Build a portable, explicit-allowlist design review ZIP. Never include business data."""

from hashlib import sha256
from pathlib import Path
from zipfile import ZIP_DEFLATED, ZipFile

ROOT = Path(__file__).resolve().parents[1]
LAB = Path("docs/design-review-2026-10")
TEXT_FILES = (
    "index.html", "preview.html", "review.css", "preview.css", "review.mjs",
    "preview.mjs", "review-store.mjs", "data.mjs", "README.md", "DESIGN_SPEC.md",
    "LAUNCH_CHECKLIST.md", "ACCEPTANCE.md", "compose.yaml",
)
START_HERE = """# Self Deepsearch · 20 套设计审核包

解压全部文件，保持目录结构。需要 Python 3.9 或更新版本，无需 Node、npm、Docker 或数据库。

在本文件所在目录运行：

```bash
python3 scripts/serve_design_review.py --port 13004
```

在同一台电脑打开 http://127.0.0.1:13004/ 。不要双击 HTML 文件。
退出服务按 Ctrl+C。服务只监听本机，不对公网开放。

20 套总览 → 进入审核 → 切换端/页面/设备 → 并排比较 → 填写意见 → 导出 JSON。
意见保存在当前浏览器，换电脑前请导出。导入会合并收藏，同键意见以导入文件为准。

这是合成数据交互原型，不是生产站；登录、发布、投诉及年龄核验不会连接真实服务。
详细说明：docs/design-review-2026-10/README.md。
此便携包不含完整 npm 项目；浏览器自动化命令需回到原项目中运行。
MANIFEST.sha256 可用于核对文件完整性。
"""


def main():
    paths = [LAB / name for name in TEXT_FILES]
    paths.extend(LAB / f"thumbs/{number:02d}-{view}.jpg" for number in range(1, 21) for view in ("public", "ops", "mobile"))
    paths.append(Path("scripts/serve_design_review.py"))
    for relative in paths:
        source = ROOT / relative
        if not source.is_file() or not source.resolve().is_relative_to(ROOT.resolve()):
            raise RuntimeError(f"Missing or unsafe review artifact: {relative}")
    output = ROOT / ".cache/design-review/self-deepsearch-design-review.zip"
    output.parent.mkdir(parents=True, exist_ok=True)
    manifest = []
    with ZipFile(output, "w", ZIP_DEFLATED, compresslevel=6) as archive:
        for relative in paths:
            body = (ROOT / relative).read_bytes()
            archive.writestr(relative.as_posix(), body)
            manifest.append(f"{sha256(body).hexdigest()}  {relative.as_posix()}")
        archive.writestr("START_HERE.md", START_HERE)
        manifest.append(f"{sha256(START_HERE.encode()).hexdigest()}  START_HERE.md")
        archive.writestr("MANIFEST.sha256", "\n".join(manifest) + "\n")
    print(f"Packaged {len(paths)} explicit artifacts: {output}")
    print(f"ZIP SHA-256: {sha256(output.read_bytes()).hexdigest()}")


if __name__ == "__main__":
    main()
