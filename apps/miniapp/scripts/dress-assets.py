#!/usr/bin/env python3
"""换装洗牌素材流水线（spec 2026-09-23-dress-shuffle §3，M4）。

源图：images/color/*（2400×1792，基准砖红插画，白底）
产出：apps/server/assets/daily/dress/color/
  {look}.png           8 张 look 人物图（键出 + 降采样 + 量化）
  outfit-{hair}.png    4 张发型人物图
  card-*.jpg           卡片缩略图（原型已验收产物直通）

三步纪律：
  键出    白→alpha（min 通道 215–240 线性），并做白底 un-blend 防暗边；
          透明底让换色滤镜只作用于服装本身
  降采样  2400×1792 → 默认 1500×1120（人物显示区 750×560rpx 的 2x，足够 3dpr）
  量化    PIL FASTOCTREE 256 色（插画扁平色，视觉无损）；pngquant 不可用时的替代

深色专属生图（墨绿/藏青/酒红/碳灰）是 M1 滤镜失败的降级备胎，客户端尚未
消费，本管线暂不处理——接入时在此扩展 COLOR_FIGURES 映射。

用法：
  python3 apps/miniapp/scripts/dress-assets.py            # 全量产出
  python3 apps/miniapp/scripts/dress-assets.py --dry-run  # 只打印计划
"""
import argparse
import shutil
from pathlib import Path

import numpy as np
from PIL import Image

ROOT = Path(__file__).resolve().parents[3]
SRC = ROOT / "images" / "color"
CARDS = ROOT / "docs" / "prototypes" / "assets" / "color"
OUT = ROOT / "apps" / "server" / "assets" / "daily" / "dress" / "color"

# look 人物图：文件名（不含扩展名）= 洗牌库 key
LOOK_SOURCES = {
    "outfit": "outfit.png",
    "ratio": "ratio.jpg",
    "fit": "fit.jpg",
    "occasion": "occasion.jpg",
    "look-shirt-skirt": "look-shirt-skirt.jpg",
    "look-hoodie-jeans": "look-hoodie-jeans.jpg",
    "look-coat": "look-coat.jpg",
    "look-trench": "look-trench.jpg",
}

# 发型人物图：中文源名 → 洗牌库 key（词汇表与 presets.ts / variant.go 同步）
HAIR_SOURCES = {
    "wave": "大波浪发型.jpg",
    "bun": "丸子头发型.png",
    "bob": "齐耳短发型.jpg",
    "long": "长直发发型.jpg",
}

# 卡片缩略图（原型已验收，直通）：look 卡 + 发型卡 + 腰线规则卡
CARD_NAMES = [f"card-{key}-v2.jpg" for key in LOOK_SOURCES] + [
    f"card-outfit-{key}-v2.jpg" for key in HAIR_SOURCES
] + ["card-rule-v2.jpg"]

# 白→alpha 判据（spec §3：min 通道 215–240 线性过渡）
KEY_LO, KEY_HI = 215, 240

DEFAULT_WIDTH = 1500  # 人物显示区 750×560rpx 的 2 倍
DEFAULT_COLORS = 256
SIZE_BUDGET_KB = 350  # spec §3 单张预算

# ---------- 包内首帧（bundle） ----------
# 4 张 hero 人物图 + 核心卡进小程序主包：首次进入也零网络，弱网/断网都能
# 立刻出人物、能洗牌（其余 look 与发型仍走 CDN + 本地缓存）。
# 尺寸 1200×896：人物在屏上最大约 245×692px（3x dpr），1200 宽仍富余。
# 160 色：扁平插画，与 256 色肉眼无差，体积再省 27%。
BUNDLE_DIR = ROOT / "apps" / "miniapp" / "src" / "assets" / "dress"
BUNDLE_WIDTH = 1200
BUNDLE_COLORS = 160
BUNDLE_LOOK_KEYS = ["outfit", "ratio", "fit", "occasion"]


def white_to_alpha(rgb: np.ndarray) -> np.ndarray:
    """白底 → 透明：alpha 由 min 通道线性映射，并把颜色从白底 un-blend（防暗边）。"""
    m = rgb.min(axis=2).astype(np.float64)
    alpha = np.clip((KEY_HI - m) / (KEY_HI - KEY_LO), 0.0, 1.0)
    a = alpha[..., None]
    safe = np.maximum(a, 1e-6)
    unblended = 255.0 - (255.0 - rgb.astype(np.float64)) / safe
    out = np.where(a > 0, unblended, 255.0)
    rgba = np.dstack([np.clip(out, 0, 255).astype(np.uint8), (alpha * 255).astype(np.uint8)])
    return rgba


def process_figure(src_path: Path, out_path: Path, width: int, colors: int) -> int:
    im = Image.open(src_path).convert("RGB")
    rgba = white_to_alpha(np.asarray(im))
    keyed = Image.fromarray(rgba, "RGBA")
    if width < keyed.width:
        height = round(keyed.height * width / keyed.width)
        keyed = keyed.resize((width, height), Image.LANCZOS)
    quantized = keyed.quantize(colors=colors, method=Image.Quantize.FASTOCTREE)
    quantized.save(out_path, optimize=True)
    return out_path.stat().st_size


def build_bundle(cards: list[str]) -> list[int]:
    """包内首帧：4 张 hero 人物图（1200 宽/160 色）+ 核心卡直通。"""
    BUNDLE_DIR.mkdir(parents=True, exist_ok=True)
    sizes = []
    for key in BUNDLE_LOOK_KEYS:
        src = SRC / LOOK_SOURCES[key]
        dst = BUNDLE_DIR / f"{key}.png"
        sizes.append(process_figure(src, dst, BUNDLE_WIDTH, BUNDLE_COLORS))
    for name in cards:
        shutil.copyfile(CARDS / name, BUNDLE_DIR / name)
    return sizes


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--width", type=int, default=DEFAULT_WIDTH)
    parser.add_argument("--colors", type=int, default=DEFAULT_COLORS)
    parser.add_argument("--bundle", action="store_true", help="同时产出包内首帧素材")
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()

    figures = {**LOOK_SOURCES, **{f"outfit-{k}": v for k, v in HAIR_SOURCES.items()}}
    plan = [(f"{key}.png", SRC / src) for key, src in figures.items()]
    plan += [(name, CARDS / name) for name in CARD_NAMES]

    missing = [str(p) for _, p in plan if not p.exists()]
    if missing:
        raise SystemExit("缺少源文件：\n  " + "\n  ".join(missing))

    print(f"产出 {len(plan)} 个文件 → {OUT}")
    if args.dry_run:
        for name, src in plan:
            print(f"  {name:32s} ← {src.name}")
        return

    OUT.mkdir(parents=True, exist_ok=True)
    over = []
    for name, src in plan:
        dst = OUT / name
        if name.endswith(".jpg"):
            shutil.copyfile(src, dst)
            size = dst.stat().st_size
        else:
            size = process_figure(src, dst, args.width, args.colors)
        kb = size // 1024
        mark = ""
        if name.endswith(".png") and kb > SIZE_BUDGET_KB:
            mark = f"  ⚠ 超 {SIZE_BUDGET_KB}KB 预算"
            over.append(name)
        print(f"  {name:32s} {kb:5d}KB{mark}")

    if over:
        print(f"\n{len(over)} 张超预算：考虑 --colors 128 或 --width 1350")

    if args.bundle:
        bundle_cards = [f"card-{key}-v2.jpg" for key in BUNDLE_LOOK_KEYS] + ["card-rule-v2.jpg"]
        print(f"\n包内首帧 → {BUNDLE_DIR}")
        for key, size in zip(BUNDLE_LOOK_KEYS, build_bundle(bundle_cards)):
            print(f"  {key}.png{' ' * 26}{size // 1024:5d}KB")
        for name in bundle_cards:
            print(f"  {name:32s}{(BUNDLE_DIR / name).stat().st_size // 1024:5d}KB")
    print("完成")


if __name__ == "__main__":
    main()
