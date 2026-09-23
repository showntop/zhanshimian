"""底图基线：验证这张线稿能不能提取出各部位（闭合性），同时产出底图 mask。
用法：python3 extract_base.py
"""
import os
import numpy as np
from PIL import Image, ImageDraw

BASE = '/data/dennyxiao/zhanshimian/docs/prototypes/assets'
SRC = os.path.join(BASE, 'layers', 'lineart-base.png')
OUT = os.path.join(BASE, 'masks-base')

a = np.asarray(Image.open(SRC).convert('L'))
H, W = a.shape
bw = np.where(a < 215, 0, 255).astype(np.uint8)      # 二值化，防跨线
img = Image.fromarray(bw, 'L')
os.makedirs(OUT, exist_ok=True)
print('底图 %dx%d' % (W, H))

def snap(x, y):
    if bw[y, x] >= 250: return x, y
    for r in range(1, 40):
        for dy in range(-r, r + 1):
            for dx in range(-r, r + 1):
                yy, xx = y + dy, x + dx
                if 0 <= yy < H and 0 <= xx < W and bw[yy, xx] >= 250:
                    return xx, yy
    return None

def ff(name, x, y):
    s = snap(x, y)
    if s is None:
        print('%-8s 种子(%d,%d)附近找不到空白' % (name, x, y)); return
    sx, sy = s
    w = img.copy(); ImageDraw.floodfill(w, (sx, sy), 128, thresh=1)
    m = (np.asarray(w) == 128); area = int(m.sum()); pct = 100.0 * area / (H * W)
    ys, xs = np.where(m)
    v = 'OK' if (area > 300 and pct < 40) else ('太小' if area <= 300 else '溢出')
    print('%-8s (%3d,%3d) %6.2f%%  bbox x%3d-%3d y%3d-%3d  %s'
          % (name, sx, sy, pct, xs.min(), xs.max(), ys.min(), ys.max(), v))
    if v == 'OK':
        rgba = np.zeros((H, W, 4), np.uint8); rgba[..., 0:3] = 255
        rgba[..., 3] = (m * 255).astype(np.uint8)
        Image.fromarray(rgba, 'RGBA').save(os.path.join(OUT, name + '.png'))

for n, x, y in [
    ('face', 208, 92), ('hairL', 172, 100), ('hairR', 245, 100),
    ('top', 208, 300), ('bottomL', 190, 600), ('bottomR', 240, 600),
    ('armL', 143, 300), ('armR', 278, 300),
    ('handL', 133, 470), ('handR', 288, 470),
    ('shoeL', 195, 790), ('shoeR', 240, 790),
]:
    ff(n, x, y)
print('\nmask ->', OUT)
