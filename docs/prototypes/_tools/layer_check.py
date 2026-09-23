#!/usr/bin/env python3
"""分层素材验收 + 层 mask 提取。

对每层（layer-hair-a.png 等）与底图做像素比对：
  1. 差异区域 = 本次重绘的区域，应**完全落在该部位的预期带内**
     越界 = 底模被改动 → 该层作废（叠上去会错位）
  2. 差异过大 = 重绘跑偏
  3. 通过的话，把差异区域直接存成该层的上色 mask（RGBA，含 alpha）

用法：python3 layer_check.py [底图路径]
"""
import os, sys, glob
import numpy as np
from PIL import Image

ROOT = '/data/dennyxiao/zhanshimian/docs/prototypes/assets'
LAYERS = os.path.join(ROOT, 'layers')
OUT = os.path.join(ROOT, 'masks-layers')

# 各部位的预期纵向范围（占全图高度的比例），留 ±5% 容差
EXPECT = {
    'hair': (0.00, 0.45),
    'top':  (0.14, 0.60),
    'bot':  (0.33, 1.01),
}
TOL = 0.05

def load(p):
    return np.asarray(Image.open(p).convert('L')).astype(int)

def main():
    base_path = sys.argv[1] if len(sys.argv) > 1 else os.path.join(LAYERS, 'lineart-base.png')
    if not os.path.exists(base_path):
        print('底图还不存在：', base_path)
        print('先出底图，再跑这个脚本做基线。')
        return
    base = load(base_path); H, W = base.shape
    print('底图 %dx%d' % (W, H))

    files = sorted(glob.glob(os.path.join(LAYERS, 'layer-*.png')))
    if not files:
        print('层目录为空：', LAYERS)
        print('（底图到位后，再出 layer-hair-*.png / layer-top-*.png / layer-bot-*.png）')
        return

    os.makedirs(OUT, exist_ok=True)
    print()
    print('%-14s %-7s %-26s %s' % ('层', '差异%', '差异范围 y(比例)', '判定'))
    for f in files:
        name = os.path.splitext(os.path.basename(f))[0]
        parts = name.split('-')
        kind = parts[1] if len(parts) > 1 else ''
        v = load(f)
        if v.shape != base.shape:
            print('%-14s 尺寸不一致 %s vs %s' % (name, v.shape, base.shape)); continue
        diff = np.abs(v - base) > 40
        n = int(diff.sum()); pct = 100.0 * n / (H * W)
        if n == 0:
            print('%-14s %-7s %-26s %s' % (name, '0.00', '-', '无差异（和底图一样？检查是否漏重绘）'))
            continue
        ys, xs = np.where(diff)
        y0, y1 = ys.min() / H, ys.max() / H
        lo, hi = EXPECT.get(kind, (0.0, 1.0))
        inside = (y0 >= lo - TOL) and (y1 <= hi + TOL)
        if not inside:   verdict = '越界！底模被改动 → 作废'
        elif pct > 30:   verdict = '差异过大 → 重绘跑偏'
        else:            verdict = 'OK'
        print('%-14s %-7.2f %.2f - %.2f (期望 %.2f-%.2f)  %s' % (name, pct, y0, y1, lo, hi, verdict))
        if verdict == 'OK':
            rgba = np.zeros((H, W, 4), np.uint8)
            rgba[..., 0:3] = 255
            rgba[..., 3] = (diff * 255).astype(np.uint8)
            Image.fromarray(rgba, 'RGBA').save(os.path.join(OUT, name + '.png'))
    print('\n通过的层 mask ->', OUT)

if __name__ == '__main__':
    main()
