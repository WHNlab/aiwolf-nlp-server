"""参加キットの公開用ZIPを作る。秘密情報・実行時キャッシュは含めない。"""
from pathlib import Path
import sys
import zipfile

root = Path(__file__).resolve().parents[1] / "web/kit/aiwolf-player"
version = (root / "VERSION").read_text().strip()
output = Path(sys.argv[1] if len(sys.argv) > 1 else "dist")
output.mkdir(parents=True, exist_ok=True)
target = output / ("aiwolf-player-" + version + ".zip")
with zipfile.ZipFile(target, "w", compression=zipfile.ZIP_DEFLATED) as archive:
    for name in ("VERSION", "SKILL.md", "GUIDE.md", "requirements.txt", "scripts/agent.py"):
        archive.write(root / name, "aiwolf-player/" + name)
print(target)
