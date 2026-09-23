"""步骤 3 · mask 差分法 v2：孔洞用「边界贴差分」判据（内部无差分是常态）。"""
import numpy as np
from PIL import Image
from scipy import ndimage

ROOT = '/data/dennyxiao/zhanshimian/docs/prototypes/assets'
BASE = '/data/dennyxiao/zhanshimian/images/lineart/_base_x.jpg/ok-2026_09_22_21_23_29_7ljkfdiwka.jpg'
LOOKS = {
    'outfit':   (f'{ROOT}/lineart/lineart-outfit.png',   0.455),
    'ratio':    (f'{ROOT}/lineart/lineart-ratio.png',    0.415),
    'fit':      (f'{ROOT}/lineart/lineart-fit.png',      None),
    'occasion': (f'{ROOT}/lineart/lineart-occasion.png', 0.545),
}
base = np.asarray(Image.open(BASE).convert('L'), dtype=np.int16)

for look, (path, waist) in LOOKS.items():
    lk = np.asarray(Image.open(path).convert('L'), dtype=np.int16)
    changed = np.abs(base - lk) > 18
    ch_d4 = ndimage.binary_dilation(changed, iterations=4)
    ch_d10 = ndimage.binary_dilation(changed, iterations=10)
    seal = ndimage.binary_dilation(lk < 205, iterations=3)
    free = ~seal
    lab, n = ndimage.label(free)
    border = np.unique(np.concatenate([lab[0], lab[-1], lab[:, 0], lab[:, -1]]))
    outside = np.isin(lab, border[border != 0])
    holes = free & ~outside
    lab_h, nh = ndimage.label(holes)
    interior = np.zeros_like(holes)
    for i in range(1, nh + 1):
        comp = lab_h == i
        area = comp.sum()
        if area < 3000:
            continue
        per = ndimage.binary_dilation(comp, iterations=2) & ~comp
        contact = (per & ch_d10).sum()
        frac = contact / max(per.sum(), 1)
        if frac > 0.30:   # 边界大部分贴着差分带 = 被新服装轮廓包围
            interior |= comp
    garment = ndimage.binary_fill_holes(
        interior | (changed & ndimage.binary_dilation(interior, iterations=20)))
    garment = ndimage.binary_closing(garment, structure=np.ones((9, 9)))
    lab_g, ng = ndimage.label(garment)
    sizes = ndimage.sum(garment, lab_g, range(1, ng + 1))
    garment = np.isin(lab_g, [i + 1 for i, s in enumerate(sizes) if s > 30000])
    garment = ndimage.binary_erosion(garment, iterations=3)
    H, W = garment.shape
    top = garment.copy()
    bottom = np.zeros_like(garment)
    if waist is not None:
        y = int(H * waist)
        top[y:, :] = False            # top = 腰线以上
        bottom[y:, :] = garment[y:, :]  # bottom = 腰线以下（显式装填）
    # waist=None（连衣裙）：top=整裙，bottom 全空，避免 zBot 压暗叠染
    import os
    os.makedirs(f'{ROOT}/masks/{look}', exist_ok=True)
    def save(mask, out):
        rgba = np.zeros((H, W, 4), dtype=np.uint8)
        rgba[..., :3][mask] = 255
        rgba[..., 3][mask] = 255
        Image.fromarray(rgba).save(out)
    save(top, f'{ROOT}/masks/{look}/top.png')
    save(bottom, f'{ROOT}/masks/{look}/bottom.png')
    print(f'{look:9s} top={top.sum():8d} bottom={bottom.sum():8d} 孔洞保留={int(interior.sum())}')

TOP_C, BOT_C = (180, 85, 60), (140, 62, 44)
for look in LOOKS:
    line = Image.open(LOOKS[look][0]).convert('L')
    W, H = line.size
    color = Image.new('RGB', (W, H), (255, 255, 255))
    for name, c in (('top', TOP_C), ('bottom', BOT_C)):
        m = Image.open(f'{ROOT}/masks/{look}/{name}.png').split()[3]
        color.paste(Image.new('RGB', (W, H), c), (0, 0), m)
    comp = np.minimum(np.asarray(color), np.asarray(line)[..., None])
    Image.fromarray(comp.astype(np.uint8)).save(f'/tmp/verify-{look}.jpg', quality=85)
print('验证图已更新')
