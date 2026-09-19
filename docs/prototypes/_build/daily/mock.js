// 每日内容池 · mock
//
// 用途：展示内容质量的「上限」，供评审用。不是生产数据。
//
// 两条硬约束写在这里，因为它们是这个功能的地基：
//   1. 没有任何一条可以要求知道用户当天穿什么 —— 那是臆想的来源
//   2. 没有任何一条评价用户的身体 —— 只说穿法、面料、配色、位置
//
// 视觉数据（visual.spec）契约：
//   swatch  { items: [{ tone, label, state: 'pick' | 'drop' }] }
//   compare { left: { label, tone }, right: { label, tone, layered? }, marker? }
//   diagram { items: [{ label, state: 'pick' | 'avoid' }] }
//   video   { durationSec, steps }
// 所有 label 必须是中文且用户能懂。曾出现过 flat-black / above-knee 这类开发者文字，
// 那是硬伤——用户不该去猜一个英文标识是什么意思。
//
// 全部 reviewed: false。原理取自通行的形象知识，句式与结构可保证，
// 但「是否真的适用于这类人」必须由形象顾问逐条确认后才能进生产池。
// 开发期素材通道。
//
// 为什么素材不进包：主包只剩约 30KB，三张图就有 1.2MB，放不下。
// 所以 Image 的 src 必须是网络地址——写相对路径（'black-flat.jpg'）小程序解析不了，会渲染为空。
//
// 这里是临时通道：本机静态服务 + 局域网 IP。使用前提：
//   1. 本机在 docs/prototypes 目录跑静态服务（端口 8900）
//   2. 微信开发者工具 → 详情 → 本地设置 → 勾选「不校验合法域名」
//   3. 真机预览需手机与电脑同一网络
// 生产必须换成 CDN / 服务端下发的完整 URL，届时删掉这个常量。
const MOCK_ASSET_BASE = 'http://192.168.31.245:8900/assets/';
function asset(name) {
    return `${MOCK_ASSET_BASE}${name}`;
}
/** 基因取值 → 中文标签，供 fit 文案拼装 */
const SKIN = {
    cool: '冷调',
    warm: '暖调',
    neutral: '中性',
};
const CONTRAST = {
    high: '五官对比强',
    medium: '五官对比适中',
    low: '五官对比柔和',
};
const SHOULDER = {
    narrow: '肩偏窄',
    medium: '肩宽适中',
    broad: '肩偏宽',
};
const FRAME = {
    small: '骨架偏小',
    medium: '骨架适中',
    large: '骨架偏大',
};
const NECK = {
    short: '颈偏短',
    medium: '颈长适中',
    long: '颈偏长',
};
const FACE = {
    round: '脸型偏圆',
    oval: '脸型偏长',
    square: '脸部轮廓偏方',
    heart: '脸型上宽下窄',
};
const HEIGHT = {
    petite: '身高中等偏下',
    average: '身高中等',
    tall: '身高中等偏上',
};
export const MOCK_CONTENTS = [
    // ---------------- 色彩 ----------------
    {
        id: 'c-white-tone',
        type: 'color',
        topic: '冬天的白，不止一种',
        lead: '本白、米白、奶油白——挂在架子上差不多，上身差很多。',
        fit: (g) => g.skinTone === 'cool'
            ? `你是${SKIN[g.skinTone]}肤色。本白和冷白贴着皮肤，气色往上走；米白、奶油白里的黄调，会把冷皮衬得发闷。`
            : g.skinTone === 'warm'
                ? `你是${SKIN[g.skinTone]}肤色。米白、奶油白会顺着你的底色走；纯冷白反而容易让你显得没血色。`
                : `你的肤色不挑冷暖，白色可以放开选——这时该看的是明度：${CONTRAST[g.contrast]}，选跟你对比度一致的那一档。`,
        why: '白色也有色温。挑白和挑蓝一样，先看冷暖，再看明度。',
        visual: {
            modality: 'swatch',
            spec: {
                items: [
                    { tone: '#FBFBF8', label: '本白', state: 'pick' },
                    { tone: '#F2F4F1', label: '冷白', state: 'pick' },
                    { tone: '#EFEADC', label: '米白', state: 'drop' },
                    { tone: '#E6DECD', label: '奶油白', state: 'drop' },
                ],
            },
            alt: '四种白色并排，本白与冷白适合，米白与奶油白不适合',
        },
        season: ['autumn', 'winter'],
        asset: 'swatch',
        dedupeKey: 'color.white.tone',
        source: 'programmatic',
        reviewed: false,
    },
    {
        id: 'c-all-black',
        type: 'color',
        topic: '全黑并不自动显瘦',
        lead: '黑色显瘦这句话，只对了一半。',
        fit: (g) => g.contrast === 'low'
            ? `你${CONTRAST[g.contrast]}。大面积的纯黑会把你整个人压成一片，反而没了轮廓——用不同材质分开层次，比用颜色更安全。`
            : `你${CONTRAST[g.contrast]}，撑得住全黑。但记得给一个亮部（领口、包或鞋），否则整体会闷。`,
        why: '显瘦靠的是轮廓和材质的一致性，不是颜色的深浅。全黑若材质也一致，会变成一个没有边界的块。',
        visual: {
            // 海报式：位置 / 大小 / 旋转全部写进 spec，改构图不用动组件
            modality: 'poster',
            spec: {
                word: { main: '全黑', sub: '不一定瘦', rotate: -7 },
                chip: { text: '同样的黑，差在材质' },
                layers: [
                    // x/y 相对视觉区，w 为 rpx 宽度；将来逐件素材到位后继续加元素即可
                    { image: asset('black-flat.jpg'), x: '4%', y: '26%', w: 400, rotate: -4 },
                    { image: asset('black-layered.jpg'), x: '50%', y: '48%', w: 336, rotate: 5 },
                ],
            },
            alt: '同样是黑，左边材质单一，右边靠材质分出层次',
        },
        asset: 'palette',
        dedupeKey: 'color.allblack.myth',
        source: 'editorial',
        reviewed: false,
    },
    {
        id: 'c-camel-trap',
        type: 'color',
        topic: '驼色是个陷阱',
        lead: '驼色大衣人人有，但不是人人都能贴近脸穿。',
        fit: (g) => g.skinTone === 'cool'
            ? `你是${SKIN[g.skinTone]}肤色，驼色的暖调贴到下巴会显黄。把驼色放在外套或下半身，内搭换成本白或灰蓝隔开。`
            : `你是${SKIN[g.skinTone]}肤色，驼色贴脸反而顺。可以放心把它放在离脸最近的位置。`,
        why: '靠近脸的颜色会直接和肤色反应。不合适的色相放远一点，问题会小很多。',
        visual: {
            modality: 'diagram',
            spec: {
                kind: 'body',
                items: [
                    { label: '贴近脸的内搭', state: 'avoid' },
                    { label: '外套', state: 'pick' },
                    { label: '下装', state: 'pick' },
                ],
            },
            alt: '驼色应避开贴脸的内搭，放在外套或下装',
        },
        season: ['autumn', 'winter'],
        asset: 'palette',
        dedupeKey: 'color.camel.proximity',
        source: 'editorial',
        reviewed: false,
    },
    {
        id: 'c-brightest',
        type: 'color',
        topic: '最亮的那个不一定最显白',
        lead: '挑"显白色"时，多数人会选最亮的一档。',
        fit: (g) => g.contrast === 'high'
            ? `你${CONTRAST[g.contrast]}。你可以驾驭高饱和、强对比的色，太柔和的颜色反而让你没精神。`
            : g.contrast === 'low'
                ? `你${CONTRAST[g.contrast]}。最亮最跳的颜色会把你的五官压住，选明度中等、饱和度低的更稳。`
                : `你${CONTRAST[g.contrast]}，两头都能穿，但别同时用——强对比留给上半身，柔和色做大面积。`,
        why: '显白是对比度匹配，不是明度越高越好。颜色和你的五官对比度打架时，赢的通常是颜色。',
        visual: {
            modality: 'swatch',
            spec: {
                items: [
                    { tone: '#2F3540', label: '强对比', state: 'pick' },
                    { tone: '#7E8A72', label: '中间调' },
                    { tone: '#D8D3C6', label: '柔和色' },
                ],
            },
            alt: '三档对比度的颜色，从强对比到柔和色',
        },
        asset: 'swatch',
        dedupeKey: 'color.contrast.match',
        source: 'programmatic',
        reviewed: false,
    },
    {
        id: 'c-three-colors',
        type: 'color',
        topic: '三色原则里，得有一个跳出来',
        lead: '全身不超过三个颜色，是被说滥的一条。真正容易漏的是后半句。',
        fit: () => '三个颜色里要有一个是"主张色"——面积小但饱和度最高。全都是中间调，配色就没有焦点。',
        why: '没有焦点的配色看起来是"没想好"，不是"低调"。',
        visual: {
            modality: 'swatch',
            spec: {
                items: [
                    { tone: '#C9CFD4', label: '底色' },
                    { tone: '#8E9A83', label: '辅助色' },
                    { tone: '#B4674D', label: '主张色', state: 'pick' },
                ],
            },
            alt: '三色配色，主张色面积小但最跳',
        },
        asset: 'palette',
        dedupeKey: 'color.three.accent',
        source: 'editorial',
        reviewed: false,
    },
    // ---------------- 版型 ----------------
    {
        id: 's-shoulder-line',
        type: 'silhouette',
        topic: '落肩在退潮，肩线回来了',
        lead: '这两季的风衣和西装都在收肩。这是个信号。',
        fit: (g) => g.shoulder === 'narrow'
            ? `你${SHOULDER[g.shoulder]}。落肩会把本来就不宽的肩再吃掉一层。选肩缝正好落在肩点的——上半身的框架立刻就有了。`
            : g.shoulder === 'broad'
                ? `你${SHOULDER[g.shoulder]}。硬挺的结构肩会再强调一次宽度。选肩线自然下落一点的，保留框架但不加量。`
                : `你${SHOULDER[g.shoulder]}，两种都能穿。判断标准是肩缝位置：落在肩点上更精神，落下 1–2cm 更松弛。`,
        why: '上装的肩线决定上半身的轮廓框架——决定它的不是袖子的宽窄，是肩缝落在哪。',
        visual: {
            modality: 'compare',
            spec: {
                left: { label: '落肩 · 肩缝下滑', tone: '#8A8F83' },
                right: { label: '肩缝落在肩点', tone: '#8E9A83', layered: true },
                marker: '肩点位置',
            },
            alt: '落肩与结构肩的对比',
        },
        weather: { maxTemp: 20 },
        season: ['autumn', 'winter'],
        asset: 'silhouette',
        dedupeKey: 'silhouette.shoulder.line',
        source: 'programmatic',
        reviewed: false,
    },
    {
        id: 's-oversize-fit',
        type: 'silhouette',
        topic: 'Oversize 不是越大越遮',
        lead: '很多人以为宽松等于藏。超出骨架的量感，会反过来吞掉人。',
        fit: (g) => g.frame === 'small'
            ? `你${FRAME[g.frame]}。大落肩、宽袖、长衣的组合会让你像被衣服穿着。留一个维度宽松就够了——比如肩正常、身宽。`
            : `你${FRAME[g.frame]}，撑得住大廓形。可以放心用一个强廓形单品做主角，其余收窄。`,
        why: '衣服的"量感"要匹配骨架的"量感"。超出太多的部分不会消失，只会把人的存在感压下去。',
        visual: {
            modality: 'compare',
            spec: {
                left: { label: '肩、身、袖一起放大', tone: '#8A8F83' },
                right: { label: '只留一处宽松', tone: '#8E9A83', layered: true },
                marker: '廓形量感',
            },
            alt: '整体放大与只留一处宽松的对比',
        },
        asset: 'silhouette',
        dedupeKey: 'silhouette.oversize.volume',
        source: 'editorial',
        reviewed: false,
    },
    {
        id: 's-waist-point',
        type: 'silhouette',
        topic: '腰线不是越高越好',
        lead: '提高腰线是常识，但提到哪才是关键。',
        fit: (g) => g.vertical === 'long_torso'
            ? '你上半身偏长，腰线可以比自然腰位略高一点，但别高过肋下——超过会显得刻意。'
            : g.vertical === 'long_leg'
                ? '你腿身比偏长，正常腰位就够，再往上提反而会失去你的优势。'
                : '腰线落在躯干最窄的那一段最稳，比"尽量高"更可靠。',
        why: '腰线的作用是给视线一个停顿点。落在最窄处才成立，过高会让比例显得不自然。',
        visual: {
            modality: 'diagram',
            spec: {
                kind: 'body',
                items: [
                    { label: '过高 · 到肋下', state: 'avoid' },
                    { label: '躯干最窄处', state: 'pick' },
                    { label: '自然腰位' },
                ],
            },
            alt: '三个腰线位置，最窄处最稳',
        },
        asset: 'proportion',
        dedupeKey: 'silhouette.waist.point',
        source: 'editorial',
        reviewed: false,
    },
    {
        id: 's-neckline',
        type: 'silhouette',
        topic: '领口不只是一个开口',
        lead: '同样是高领季，领口的形状决定了别人看到的颈长。',
        fit: (g) => {
            const shape = FACE[g.face];
            if (g.neck === 'short') {
                return `你${NECK[g.neck]}、${shape}。全包的高领会从下巴直接连到衣服，脖子就没了。换成小 V 领，或只到下巴下两指的半高领。`;
            }
            if (g.neck === 'long') {
                return `你${NECK[g.neck]}，高领反而能平衡。可以把领口做满，不用刻意留颈部。`;
            }
            return `你${NECK[g.neck]}、${shape}。半高领最稳——有保暖，也留出颈部的那一段。`;
        },
        why: '领口决定了露出的颈长，进而影响脸部的视觉大小。它是个比例问题，不是保暖问题。',
        visual: {
            modality: 'compare',
            spec: {
                left: { label: '全包高领 · 不留颈', tone: '#8A8F83' },
                right: { label: '半高领 / 小 V 领', tone: '#8E9A83', layered: true },
                marker: '露出的颈长',
            },
            alt: '全包高领与半高领的对比',
        },
        weather: { maxTemp: 16 },
        season: ['autumn', 'winter'],
        asset: 'silhouette',
        dedupeKey: 'silhouette.neckline.length',
        source: 'programmatic',
        reviewed: false,
    },
    {
        id: 's-wrist',
        type: 'silhouette',
        topic: '露腕，是最便宜的显瘦',
        lead: '不用买新衣服，袖子往上一点就够。',
        fit: () => '把袖口推到腕骨上方，或者选九分袖。这一小段露出的皮肤，会给上半身的轮廓一个收尾。',
        why: '手腕是上肢最细的关节。露出它，等于在轮廓结束处给了一个"细"的信号。',
        visual: {
            modality: 'diagram',
            spec: {
                kind: 'scale',
                items: [
                    { label: '盖住手腕' },
                    { label: '刚到腕骨' },
                    { label: '腕骨上方', state: 'pick' },
                ],
            },
            alt: '袖长三个位置，腕骨上方最利落',
        },
        asset: 'silhouette',
        dedupeKey: 'silhouette.sleeve.wrist',
        source: 'programmatic',
        reviewed: false,
    },
    // ---------------- 比例 ----------------
    {
        id: 'p-cut-line',
        type: 'proportion',
        topic: '显矮的元凶是截断，不是身高',
        lead: '深色上衣 + 浅色裤子 + 黑鞋，视觉上被切成三段。',
        fit: (g) => g.heightBand === 'petite'
            ? `你${HEIGHT[g.heightBand]}。让下装和鞋同色，腿在视觉上就不断。冬天穿靴子最容易做到——靴子和裤子一个色就行。`
            : `你${HEIGHT[g.heightBand]}，比例有余量。但仍建议避开强对比的横向分割，它会让任何身高都显短。`,
        why: '真正截断比例的是强对比，不是身高本身。同色延伸不断线。',
        visual: {
            modality: 'compare',
            spec: {
                left: { label: '上下截成三段', tone: '#3F463C' },
                right: { label: '下装与鞋同色', tone: '#4A5245', layered: true },
                marker: '视线是否断线',
            },
            alt: '三段配色与同色延伸的对比',
        },
        asset: 'palette',
        dedupeKey: 'proportion.cut.continuous',
        source: 'programmatic',
        reviewed: false,
    },
    {
        id: 'p-coat-length',
        type: 'proportion',
        topic: '外套下摆停在哪，比长度数字重要',
        lead: '同样一件大衣，下摆落点差几厘米，效果差很多。',
        fit: (g) => g.heightBand === 'petite'
            ? `你${HEIGHT[g.heightBand]}。下摆避开小腿最粗的那一段，落在膝上或脚踝上方更稳。`
            : `你${HEIGHT[g.heightBand]}，长款撑得住。注意别让下摆正好停在小腿中段——那是所有身高都该避开的落点。`,
        why: '下摆落在身体最宽处会横向切断。避开宽度峰值，比追求某个长度数字更可靠。',
        visual: {
            modality: 'diagram',
            spec: {
                kind: 'body',
                items: [
                    { label: '膝上', state: 'pick' },
                    { label: '小腿中段', state: 'avoid' },
                    { label: '脚踝附近', state: 'pick' },
                ],
            },
            alt: '外套下摆三个落点，小腿中段应避开',
        },
        weather: { maxTemp: 15 },
        season: ['autumn', 'winter'],
        asset: 'proportion',
        dedupeKey: 'proportion.coat.hem',
        source: 'editorial',
        reviewed: false,
    },
    {
        id: 'p-bag-position',
        type: 'proportion',
        topic: '包背在哪，重心就在哪',
        lead: '包不只是装东西，它是全身最显眼的一个点。',
        fit: (g) => g.heightBand === 'petite'
            ? `你${HEIGHT[g.heightBand]}。包带别太长——落点越低，重心越往下。斜挎时让包落在腰线附近。`
            : '包的落点决定别人的视线停在哪。想强调上半身就背包在高处，想拉长就让它靠近腰线。',
        why: '包是全身唯一一个可以随意移动的"重点"。它的落点会成为视觉重心。',
        visual: {
            modality: 'diagram',
            spec: {
                kind: 'body',
                image: asset('figure-body.png'),
                // 顺序即空间关系：自上而下渲染，必须按真实高度排
                items: [
                    { label: '胸前' },
                    { label: '腰线附近', state: 'pick' },
                    { label: '胯部' },
                ],
            },
            alt: '位置轴自上而下：胸前、腰线附近、胯部，腰线附近最稳',
        },
        asset: 'proportion',
        dedupeKey: 'proportion.bag.position',
        source: 'programmatic',
        reviewed: false,
    },
    {
        id: 'p-ankle',
        type: 'proportion',
        topic: '裤长露踝，比例立刻不一样',
        lead: '同样的裤子，卷一道边就是两条裤子。',
        fit: () => '裤脚落在脚踝骨上方，或者卷一道边露出来。脚踝是小腿最细处，露出它等于给下半身一个收尾。',
        why: '和露腕同理：在最细的关节处收口，轮廓会显得更利落。',
        visual: {
            modality: 'compare',
            spec: {
                left: { label: '盖住脚踝', tone: '#8A8F83' },
                right: { label: '露出脚踝', tone: '#8E9A83', layered: true },
                marker: '裤长',
            },
            alt: '长裤与九分裤的对比',
        },
        weather: { minTemp: 12 },
        asset: 'silhouette',
        dedupeKey: 'proportion.pants.ankle',
        source: 'programmatic',
        reviewed: false,
    },
    // ---------------- 面料 ----------------
    {
        id: 'f-drape',
        type: 'fabric',
        topic: '冬天的厚，不等于硬',
        lead: '同样是厚，垂下去的和撑起来的，穿在小骨架上是两回事。',
        fit: (g) => g.frame === 'small'
            ? `你${FRAME[g.frame]}。硬挺的厚面料会自己撑出一个形状，把你套在里面。垂感好的——细针织、薄羊毛——是顺着你走。`
            : `你${FRAME[g.frame]}，撑得住有结构的外套。硬挺面料能给你轮廓，不会压过你。`,
        why: '面料的"量感"要匹配骨架的"量感"。不是越厚越暖，也不是越挺越有型。',
        visual: {
            modality: 'compare',
            spec: {
                left: { label: '硬挺 · 自己撑出形状', tone: '#8A8F83' },
                right: { label: '垂感 · 顺着你走', tone: '#8E9A83', layered: true },
                marker: '面料量感',
            },
            alt: '硬挺面料与垂感面料的对比',
        },
        weather: { maxTemp: 18 },
        season: ['autumn', 'winter'],
        asset: 'fabric',
        dedupeKey: 'fabric.drape.volume',
        source: 'programmatic',
        reviewed: false,
    },
    {
        id: 'f-sheen',
        type: 'fabric',
        topic: '光泽会放大体积',
        lead: '缎面、漆皮、金属感——好看，但它们有物理副作用。',
        fit: (g) => g.frame === 'large'
            ? `你${FRAME[g.frame]}。光泽面料会再放大一次体积，建议放在配饰或小面积处，别做大面积主材。`
            : `你${FRAME[g.frame]}，小面积光泽是加分项。一个缎面包或一双漆皮鞋就够，不必全身。`,
        why: '反光会让轮廓边界变模糊，体积看起来更大。哑光收边界，光泽扩边界。',
        visual: {
            modality: 'compare',
            spec: {
                left: { label: '哑光 · 收边界', tone: '#5A6156' },
                right: { label: '光泽 · 扩边界', tone: '#8E9A83', layered: true },
                marker: '反光',
            },
            alt: '哑光与光泽面料的对比',
        },
        asset: 'fabric',
        dedupeKey: 'fabric.sheen.volume',
        source: 'editorial',
        reviewed: false,
    },
    {
        id: 'f-knit-gauge',
        type: 'fabric',
        topic: '针织的粗细，本身就是体积',
        lead: '粗针和细针，差的不是保暖，是占地。',
        fit: (g) => g.frame === 'small'
            ? `你${FRAME[g.frame]}。粗针毛衣的纹理本身就有体积，会往外扩。选细针、平针，保暖靠层数不靠粗细。`
            : `你${FRAME[g.frame]}，粗针能撑得住，可以拿它当造型主角。`,
        why: '针距越粗，面料表面越不平整，视觉体积越大。保暖靠空气层，不靠纱线粗细。',
        visual: {
            modality: 'compare',
            spec: {
                left: { label: '粗针 · 纹理有体积', tone: '#8A8F83' },
                right: { label: '细针 · 平整', tone: '#8E9A83', layered: true },
                marker: '针距',
            },
            alt: '粗针与细针的对比',
        },
        weather: { maxTemp: 15 },
        season: ['autumn', 'winter'],
        asset: 'fabric',
        dedupeKey: 'fabric.knit.gauge',
        source: 'editorial',
        reviewed: false,
    },
    // ---------------- 场合 ----------------
    {
        id: 'o-interview-degree',
        type: 'occasion',
        topic: '面试的正式度，差一级比差三级稳',
        lead: '多数人要么穿得不够，要么用力过猛。',
        fit: () => '比目标岗位的日常着装高一级就够。穿得太正式会让人觉得你在演，不够则会显得不重视——前者更难挽回。',
        why: '正式度是个相对值。它衡量的是你和"那个场合的日常"的距离，不是绝对的正装程度。',
        visual: {
            modality: 'diagram',
            spec: {
                kind: 'scale',
                items: [
                    { label: '休闲' },
                    { label: 'Smart Casual', state: 'pick' },
                    { label: '正式' },
                    { label: '礼服' },
                ],
            },
            alt: '正式度刻度，推荐 Smart Casual',
        },
        dayType: ['weekday'],
        asset: 'outfit',
        dedupeKey: 'occasion.interview.formality',
        source: 'editorial',
        reviewed: false,
    },
    // ---------------- 教程 ----------------
    {
        id: 'h-sleeve-roll',
        type: 'howto',
        topic: '袖子怎么卷，才不会滑下来',
        lead: '卷袖是日常动作，但多数人卷完半小时就松了。',
        fit: () => '先翻一次到肘下，再把袖口往上折到刚好盖住翻边。关键是第二折要压住第一折的边缘——这样它自己会锁住。',
        why: '两折互相压住才有摩擦力。只折一次的话，没有任何东西阻止它滑回去。',
        visual: {
            modality: 'video',
            spec: { durationSec: 12, steps: 2 },
            alt: '卷袖两步演示：翻到肘下，再折回盖住翻边',
        },
        asset: 'silhouette',
        dedupeKey: 'howto.sleeve.roll',
        source: 'editorial',
        reviewed: false,
    },
];
/** 默认演示基因：服务端 StyleGene 接口就绪前，客户端用它渲染 */
export const MOCK_GENE_DEFAULT = {
    skinTone: 'cool', contrast: 'medium', shoulder: 'narrow', frame: 'small',
    vertical: 'balanced', neck: 'short', face: 'round', heightBand: 'petite',
};
/** 演示用的虚拟基因，供原型与评审切换查看个性化效果 */
export const MOCK_GENES = {
    '冷调 · 小骨架 · 肩窄': MOCK_GENE_DEFAULT,
    '暖调 · 大骨架 · 肩宽': {
        skinTone: 'warm', contrast: 'high', shoulder: 'broad', frame: 'large',
        vertical: 'long_leg', neck: 'long', face: 'square', heightBand: 'tall',
    },
    '中性 · 中等': {
        skinTone: 'neutral', contrast: 'medium', shoulder: 'medium', frame: 'medium',
        vertical: 'balanced', neck: 'medium', face: 'oval', heightBand: 'average',
    },
};
