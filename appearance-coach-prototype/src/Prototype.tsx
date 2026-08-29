import { useEffect, useMemo, useState, type ReactNode } from "react";
import {
  ArrowLeftIcon,
  CameraIcon,
  CheckIcon,
  ChevronRightIcon,
  FileTextIcon,
  HeartIcon,
  HomeIcon,
  MagicWandIcon,
  MixIcon,
  PersonIcon,
  Share2Icon,
  SunIcon,
} from "@radix-ui/react-icons";
import { BottomSheet, FlowStack, MobileScroll, type FlowControls, type FlowScreen } from "./mobile";

type LookId = "natural" | "sharp" | "warm";

const looks = {
  natural: {
    id: "natural" as const,
    name: "自然通勤",
    image: "/assets/looks/natural.png",
    summary: "顺直发 · 自然眉眼 · 轻松线条",
    reason: "保留亲和感，适合创意或偏轻松的团队氛围。",
  },
  sharp: {
    id: "sharp" as const,
    name: "精神利落",
    image: "/assets/looks/sharp.png",
    summary: "锁骨发 · 提亮眉眼 · 强化肩线",
    reason: "让视线向上集中，第一眼更有精神与可信度。",
  },
  warm: {
    id: "warm" as const,
    name: "温柔专业",
    image: "/assets/looks/warm.png",
    summary: "空气卷 · 暖调妆容 · 柔和配色",
    reason: "专业感更柔和，适合需要沟通与共情的岗位。",
  },
};

function BrandHeader({ flow, title, step }: { flow?: FlowControls; title?: string; step?: string }) {
  return (
    <div className="brand-header">
      {flow?.canGoBack ? (
        <button className="icon-button" type="button" onClick={flow.pop} aria-label="返回">
          <ArrowLeftIcon />
        </button>
      ) : (
        <span className="wordmark">见我</span>
      )}
      {title ? <span className="header-title">{title}</span> : null}
      {step ? <span className="header-step">{step}</span> : <span className="header-spacer" />}
    </div>
  );
}

function PrimaryButton({ children, onClick, disabled = false }: { children: ReactNode; onClick: () => void; disabled?: boolean }) {
  return (
    <button className="primary-button" type="button" onClick={onClick} disabled={disabled}>
      <span>{children}</span>
      <ChevronRightIcon />
    </button>
  );
}

type AppTab = "home" | "plans" | "mine";

function AppTabBar({ flow, active }: { flow: FlowControls; active: AppTab }) {
  const go = (target: AppTab) => {
    if (target === active) return;
    if (target === "home") flow.replace(makeHomeScreen());
    if (target === "plans") flow.push(makePlansTabScreen());
    if (target === "mine") flow.push(makeArchiveScreen());
  };
  return (
    <nav className="app-tab-bar" aria-label="主要导航">
      {([
        ["home", "首页", <HomeIcon />],
        ["plans", "方案", <FileTextIcon />],
        ["mine", "我的", <PersonIcon />],
      ] as const).map(([id, label, icon]) => (
        <button type="button" key={id} className={active === id ? "active" : ""} onClick={() => go(id)}>
          {icon}<span>{label}</span>
        </button>
      ))}
    </nav>
  );
}

function makeHomeScreen(): FlowScreen {
  return {
    id: "home",
    footerHeight: 62,
    footer: (flow) => <AppTabBar flow={flow} active="home" />,
    render: (flow) => <HomeScreen flow={flow} />,
  };
}

function HomeScreen({ flow }: { flow: FlowControls }) {
  const scenes = [
    ["面试", "精神可信", <FileTextIcon />],
    ["婚礼", "得体上镜", <MagicWandIcon />],
    ["约会", "自然有记忆点", <HeartIcon />],
    ["日常", "省心耐看", <SunIcon />],
  ] as const;
  const tools = [
    ["hair", "发型预览", "先看再决定", <MagicWandIcon />],
    ["outfit", "穿搭诊断", "今天怎么改", <FileTextIcon />],
    ["purchase", "购买判断", "这件适合吗", <MixIcon />],
  ] as const;
  return (
    <MobileScroll className="app-screen product-home-scroll">
      <main className="product-home">
        <BrandHeader />
        <section className="product-greeting">
          <div><small>私人形象顾问</small><strong>上午好，今天想解决什么？</strong></div>
          <span><i />档案可用</span>
        </section>

        <section className="profile-workspace-card">
          <div className="profile-workspace-main">
            <img src={looks.natural.image} alt="当前形象档案" />
            <div><small>我的形象档案</small><strong>自然亲和 · 稳重克制</strong><span>最近分析已保存，可直接生成新场合方案</span></div>
          </div>
          <div className="profile-workspace-actions">
            <button type="button" onClick={() => flow.push(makePreviewScreen())}>查看分析报告</button>
            <button type="button" className="accent" onClick={() => flow.push(makeCaptureScreen())}>更新形象档案</button>
          </div>
          <button type="button" className="today-advice" onClick={() => flow.push(makePreviewScreen())}>
            <MagicWandIcon /><span><small>今天最值得先做</small><strong>提高发型重心，露出肩颈会更利落</strong></span><ChevronRightIcon />
          </button>
        </section>

        <ProductSection title="快捷工具" note="直接解决眼前的一件事">
          <div className="quick-tool-grid">
            {tools.map(([id, label, note, icon], index) => (
              <button type="button" key={id} onClick={() => flow.push(makeToolScreen(id))}>
                <span className="quick-tool-icon">{icon}</span><strong>{label}</strong><small>{note}</small>
                {index === 0 ? <em>推荐</em> : null}
              </button>
            ))}
          </div>
        </ProductSection>

        <ProductSection title="按场合开始" note="只补充一次目标，不重复建档">
          <div className="scene-card-rail">
            {scenes.map(([label, note, icon]) => (
              <button type="button" key={label} onClick={() => flow.push(makeSceneScreen(label))}>
                <span>{icon}</span><div><strong>{label}</strong><small>{note}</small></div><ChevronRightIcon />
              </button>
            ))}
          </div>
        </ProductSection>

        <ProductSection title="最近方案" note="当前与方案一眼对比" action="全部方案" onAction={() => flow.push(makePreviewScreen())}>
          <button type="button" className="recent-plan-card" onClick={() => flow.push(makePreviewScreen())}>
            <div className="recent-plan-images"><div><img src={looks.natural.image} alt="当前" /><span>当前</span></div><div><img src={looks.sharp.image} alt="方案" /><span>方案</span></div><i>→</i></div>
            <div className="recent-plan-copy"><span><strong>清晰利落</strong><small>发型 · 妆容 · 穿搭已组合</small></span><ChevronRightIcon /></div>
          </button>
        </ProductSection>

        <button type="button" className="lab-banner" onClick={() => flow.push(makeLabScreen())}>
          <MagicWandIcon /><span><strong>体验实验室</strong><small>发型 AR、3D 形象与试衣能力预览</small></span><em>内测</em><ChevronRightIcon />
        </button>
      </main>
    </MobileScroll>
  );
}

function ProductSection({ title, note, action, onAction, children }: { title: string; note: string; action?: string; onAction?: () => void; children: ReactNode }) {
  return (
    <section className="product-section-block">
      <header><div><strong>{title}</strong><small>{note}</small></div>{action ? <button type="button" onClick={onAction}>{action}</button> : null}</header>
      {children}
    </section>
  );
}

function makeToolScreen(kind: "hair" | "outfit" | "purchase"): FlowScreen {
  return { id: `tool-${kind}`, render: (flow) => <ToolScreen flow={flow} kind={kind} /> };
}

function ToolScreen({ flow, kind }: { flow: FlowControls; kind: "hair" | "outfit" | "purchase" }) {
  const [selected, setSelected] = useState<LookId>(kind === "hair" ? "sharp" : "natural");
  const [analyzed, setAnalyzed] = useState(kind === "hair");
  const title = kind === "hair" ? "发型预览" : kind === "outfit" ? "穿搭诊断" : "购买判断";
  if (kind === "hair") {
    const active = looks[selected];
    return (
      <MobileScroll className="app-screen"><main className="page app-tool-page">
        <BrandHeader flow={flow} title={title} />
        <div className="tool-page-head"><div><strong>先看效果，再决定剪不剪</strong><small>基于你的脸型与头肩比例推荐</small></div><em>AI 预览</em></div>
        <div className="hair-tool-hero"><img src={active.image} alt={active.name} /><span>{active.name}</span><small>模拟效果，仅供参考</small></div>
        <div className="hair-style-grid">{(Object.values(looks) as typeof looks[LookId][]).map((look) => <button type="button" key={look.id} className={selected === look.id ? "active" : ""} onClick={() => setSelected(look.id)}><img src={look.image} alt="" /><span>{look.name}</span></button>)}</div>
        <div className="tool-insight"><MagicWandIcon /><div><strong>为什么适合你</strong><p>{active.reason}</p></div></div>
        <PrimaryButton onClick={() => window.alert("已保存到方案")}>保存这个发型</PrimaryButton>
      </main></MobileScroll>
    );
  }
  return (
    <MobileScroll className="app-screen"><main className="page app-tool-page">
      <BrandHeader flow={flow} title={title} />
      <div className="tool-page-head"><div><strong>{kind === "outfit" ? "上传今天的穿搭" : "买之前，先看它适不适合你"}</strong><small>{kind === "outfit" ? "只指出最值得调整的一处" : "结合你的档案和使用场景判断"}</small></div></div>
      <button type="button" className={`tool-upload-card ${analyzed ? "filled" : ""}`} onClick={() => setAnalyzed(true)}>
        {analyzed ? <img src={kind === "outfit" ? looks.natural.image : looks.warm.image} alt="示例" /> : <><CameraIcon /><strong>拍照或从相册选择</strong><small>点击使用示例体验</small></>}
      </button>
      {analyzed ? <div className="tool-result-card"><span>{kind === "outfit" ? "最值得先改" : "结论：比较适合"}</span><strong>{kind === "outfit" ? "把深色内搭换成象牙白" : "适合日常，但建议搭配挺括下装"}</strong><p>{kind === "outfit" ? "无需更换整套衣服，就能让上半身更轻盈。" : "清晰肩线有利于头肩比例，颜色也容易与现有衣橱组合。"}</p></div> : null}
      <PrimaryButton onClick={() => analyzed ? flow.push(makePreviewScreen()) : setAnalyzed(true)}>{analyzed ? "生成替换方案" : "开始判断"}</PrimaryButton>
    </main></MobileScroll>
  );
}

function makeLabScreen(): FlowScreen {
  return { id: "lab", render: (flow) => <LabScreen flow={flow} /> };
}

function LabScreen({ flow }: { flow: FlowControls }) {
  const features = [
    ["发型与妆容 AR", "内测", looks.sharp.image, "实时切换发型轮廓、发色与眉眼重点。"],
    ["3D 形象 Lite", "开发中", looks.natural.image, "表达比例和穿搭轮廓，不承诺精确测量。"],
    ["上半身试衣", "排队中", looks.warm.image, "先支持外套和上衣，不做完整商城。"],
  ] as const;
  return <MobileScroll className="app-screen"><main className="page lab-prototype-page"><BrandHeader flow={flow} title="体验实验室" /><div className="tool-page-head"><div><strong>这里放“哇塞”，不打断核心流程</strong><small>实验能力都会明确标注 AI 生成</small></div></div>{features.map(([name, status, image, note], index) => <article className={`lab-prototype-card ${index === 0 ? "live" : ""}`} key={name}><img src={image} alt="" /><div><span><strong>{name}</strong><em>{status}</em></span><p>{note}</p><button type="button" onClick={() => window.alert(index === 0 ? "发型 AR 内测说明" : "已加入体验名单")}>{index === 0 ? "立即体验" : "预约体验"}</button></div></article>)}</main></MobileScroll>;
}

function makeArchiveScreen(): FlowScreen {
  return { id: "archive", footerHeight: 62, footer: (flow) => <AppTabBar flow={flow} active="mine" />, render: (flow) => <ArchiveScreen flow={flow} /> };
}

function ArchiveScreen({ flow }: { flow: FlowControls }) {
  return <MobileScroll className="app-screen"><main className="page archive-prototype-page"><BrandHeader flow={flow} title="我的档案" /><div className="archive-profile"><img src={looks.natural.image} alt="" /><div><strong>我的形象档案</strong><small>照片与建议只对你可见</small></div></div><div className="archive-list"><button type="button" onClick={() => flow.push(makePreviewScreen())}><span><strong>最近的分析报告</strong><small>自然亲和 · 稳重克制</small></span><ChevronRightIcon /></button><button type="button" onClick={() => flow.push(makePreviewScreen())}><span><strong>已保存的方案</strong><small>3 套方案 · 1 套执行中</small></span><ChevronRightIcon /></button><button type="button"><span><strong>身体数据</strong><small>身高已填写，体重与三围选填</small></span><ChevronRightIcon /></button><button type="button" onClick={() => flow.push(makeLabScreen())}><span><strong>体验实验室</strong><small>已预约 3D 形象 Lite</small></span><ChevronRightIcon /></button></div></main></MobileScroll>;
}

function SceneScreen({ flow, scene }: { flow: FlowControls; scene: string }) {
  const [time, setTime] = useState("1 周后");
  const [budget, setBudget] = useState("500–1500");
  const [formality, setFormality] = useState("得体");
  const [impression, setImpression] = useState("更可信");
  return (
    <MobileScroll className="app-screen">
      <main className="page scene-brief-page">
        <BrandHeader flow={flow} title="场合需求" />
        <section className="scene-brief-summary">
          <span><FileTextIcon /></span>
          <div><strong>{scene}方案</strong><small>补充 4 个选择，约 30 秒</small></div>
          <em>复用档案</em>
        </section>
        <div className="brief-form-card">
          <ChoiceSection title="什么时候需要？" step="1 / 4" options={["今天", "3 天内", "1 周后", "还没确定"]} value={time} onChange={setTime} />
          <ChoiceSection title="本次预算" step="2 / 4" options={["500 以内", "500–1500", "1500 以上"]} value={budget} onChange={setBudget} />
          <ChoiceSection title="正式程度" step="3 / 4" options={["轻松", "得体", "正式"]} value={formality} onChange={setFormality} />
          <ChoiceSection title="最想呈现的感觉" step="4 / 4" options={["更有精神", "更可信", "更自然", "有记忆点"]} value={impression} onChange={setImpression} />
        </div>
        <p className="brief-reuse-note"><CheckIcon />不会重复索要照片和身体数据</p>
        <div className="bottom-action compact">
          <PrimaryButton onClick={() => flow.push(makePreviewScreen())}>生成{scene}方案</PrimaryButton>
        </div>
      </main>
    </MobileScroll>
  );
}

function ChoiceSection({ title, step, options, value, onChange }: { title: string; step: string; options: string[]; value: string; onChange: (value: string) => void }) {
  return <section className="choice-section"><header><strong>{title}</strong><small>{step}</small></header><div>{options.map((option) => <button type="button" key={option} className={value === option ? "active" : ""} onClick={() => onChange(option)}>{option}</button>)}</div></section>;
}

function makeCaptureScreen(): FlowScreen {
  return { id: "capture", render: (flow) => <CaptureScreen flow={flow} /> };
}

function CaptureScreen({ flow }: { flow: FlowControls }) {
  const [uploaded, setUploaded] = useState<string[]>([]);
  const shots = [
    ["正脸", "看清五官与肤色", "face"],
    ["45° 侧脸", "判断轮廓与发型", "side"],
    ["正面全身", "分析头肩与身材比例", "body"],
  ];
  return (
    <MobileScroll className="app-screen">
      <main className="page capture-page">
        <BrandHeader flow={flow} title="创建形象档案" step="02 / 05" />
        <section className="section-heading">
          <p className="eyebrow">三张自然光照片</p>
          <h1>让建议真正像你</h1>
          <p>不用化妆，也不需要刻意摆姿势。</p>
        </section>

        <div className="capture-list">
          {shots.map(([label, desc, id], index) => {
            const done = uploaded.includes(id);
            return (
              <button
                key={id}
                type="button"
                className={`capture-row ${done ? "done" : ""}`}
                onClick={() => setUploaded((items) => done ? items.filter((item) => item !== id) : [...items, id])}
              >
                <span className="capture-preview">
                  {done ? <img src={looks.sharp.image} alt="已上传示例" style={{ objectPosition: index === 2 ? "50% 18%" : "50% 8%" }} /> : <CameraIcon />}
                </span>
                <span className="capture-copy"><strong>{label}</strong><small>{done ? "已完成，可重新拍摄" : desc}</small></span>
                <span className="capture-state">{done ? <CheckIcon /> : <ChevronRightIcon />}</span>
              </button>
            );
          })}
        </div>

        <button className="demo-fill" type="button" onClick={() => setUploaded(["face", "side", "body"])}>
          使用示例照片体验
        </button>
        <div className="bottom-action compact">
          <PrimaryButton disabled={uploaded.length < 3} onClick={() => flow.push(makeProfileScreen())}>继续补充资料</PrimaryButton>
          <p>{uploaded.length} / 3 张已完成</p>
        </div>
      </main>
    </MobileScroll>
  );
}

function makeProfileScreen(): FlowScreen {
  return { id: "profile", render: (flow) => <ProfileScreen flow={flow} /> };
}

function ProfileScreen({ flow }: { flow: FlowControls }) {
  const [height, setHeight] = useState(165);
  const [role, setRole] = useState("产品经理");
  const [budget, setBudget] = useState("500–1500");
  return (
    <MobileScroll className="app-screen">
      <main className="page profile-page">
        <BrandHeader flow={flow} title="补充资料" step="03 / 05" />
        <section className="section-heading">
          <p className="eyebrow">少一点填写，多一点准确</p>
          <h1>告诉我你的现实条件</h1>
          <p>体重与三围不是第一阶段的必填项。</p>
        </section>

        <div className="form-section">
          <label>身高</label>
          <div className="stepper">
            <button type="button" onClick={() => setHeight((value) => Math.max(145, value - 1))}>−</button>
            <strong>{height}<small> cm</small></strong>
            <button type="button" onClick={() => setHeight((value) => Math.min(185, value + 1))}>＋</button>
          </div>
        </div>

        <div className="form-section">
          <label>职业</label>
          <div className="chip-group">
            {["产品经理", "设计师", "咨询顾问", "其他"].map((item) => (
              <button type="button" key={item} className={role === item ? "active" : ""} onClick={() => setRole(item)}>{item}</button>
            ))}
          </div>
        </div>

        <div className="form-section">
          <label>本次形象预算</label>
          <div className="chip-group">
            {["500以内", "500–1500", "1500以上"].map((item) => (
              <button type="button" key={item} className={budget === item ? "active" : ""} onClick={() => setBudget(item)}>{item}</button>
            ))}
          </div>
        </div>

        <div className="bottom-action compact">
          <PrimaryButton onClick={() => flow.push(makeAnalysisScreen())}>生成我的方案</PrimaryButton>
          <p>数据可随时删除</p>
        </div>
      </main>
    </MobileScroll>
  );
}

function makeAnalysisScreen(): FlowScreen {
  return { id: "analysis", render: (flow) => <AnalysisScreen flow={flow} /> };
}

function AnalysisScreen({ flow }: { flow: FlowControls }) {
  const [progress, setProgress] = useState(18);
  useEffect(() => {
    const timer = window.setInterval(() => setProgress((value) => Math.min(100, value + 9)), 130);
    return () => window.clearInterval(timer);
  }, []);
  return (
    <MobileScroll className="app-screen">
      <main className="page analysis-page">
        <BrandHeader flow={flow} title="正在分析" step="04 / 05" />
        <div className="analysis-portrait-wrap">
          <img className="analysis-portrait" src={looks.sharp.image} alt="小林的形象照片" />
          <span className="scan-line" />
        </div>
        <section className="analysis-copy">
          <p className="eyebrow">正在组合你的专属方案</p>
          <h1>{progress < 55 ? "读取面部与头肩比例" : progress < 90 ? "匹配面试场景与预算" : "三套方案已经准备好"}</h1>
          <div className="progress-track"><span style={{ width: `${progress}%` }} /></div>
          <p>{progress}%</p>
        </section>
        {progress >= 100 ? (
          <div className="bottom-action compact reveal-action">
            <PrimaryButton onClick={() => flow.push(makePreviewScreen())}>查看我的 3 套方案</PrimaryButton>
          </div>
        ) : null}
      </main>
    </MobileScroll>
  );
}

function makePreviewScreen(): FlowScreen {
  return { id: "preview", render: (flow) => <PreviewScreen flow={flow} /> };
}

function makePlansTabScreen(): FlowScreen {
  return { id: "plans-tab", footerHeight: 62, footer: (flow) => <AppTabBar flow={flow} active="plans" />, render: (flow) => <PreviewScreen flow={flow} asTab /> };
}

function PreviewScreen({ flow, asTab = false }: { flow: FlowControls; asTab?: boolean }) {
  const [selected, setSelected] = useState<LookId>("sharp");
  const [showOriginal, setShowOriginal] = useState(false);
  const active = looks[selected];
  return (
    <MobileScroll className="app-screen preview-scroll">
      <main className={`preview-page ${asTab ? "with-tab" : ""}`}>
        <BrandHeader flow={flow} title="面试形象" />
        <div className="hero-preview">
          <img key={`${selected}-${showOriginal}`} src={showOriginal ? looks.natural.image : active.image} alt={`${active.name}形象预览`} />
          <div className="comparison-toggle" role="group" aria-label="原本与方案对比">
            <button type="button" className={showOriginal ? "active" : ""} onClick={() => setShowOriginal(true)}>原本</button>
            <button type="button" className={!showOriginal ? "active" : ""} onClick={() => setShowOriginal(false)}>方案</button>
          </div>
          <span className="preview-note">AI 风格预览</span>
        </div>

        <div className="look-rail" role="radiogroup" aria-label="选择形象方案">
          {(Object.values(looks) as typeof looks[LookId][]).map((look) => (
            <button
              type="button"
              key={look.id}
              className={`look-card ${selected === look.id ? "selected" : ""}`}
              onClick={() => { setSelected(look.id); setShowOriginal(false); }}
              role="radio"
              aria-checked={selected === look.id}
            >
              <img src={look.image} alt="" />
              <span>{look.name}</span>
            </button>
          ))}
        </div>

        <div className="change-summary">
          <MagicWandIcon />
          <span>{active.summary}</span>
        </div>
        <p className="reason-copy">{active.reason}</p>

        <div className="preview-actions">
          <PrimaryButton onClick={() => flow.push(makePlanScreen(selected))}>选这套</PrimaryButton>
          <button className="secondary-button" type="button" onClick={() => flow.push(makePlanScreen(selected))}>查看执行清单</button>
        </div>
      </main>
    </MobileScroll>
  );
}

function makePlanScreen(selected: LookId): FlowScreen {
  return { id: "plan", render: (flow) => <PlanScreen flow={flow} initialLook={selected} /> };
}

function PlanScreen({ flow, initialLook }: { flow: FlowControls; initialLook: LookId }) {
  const [sheetOpen, setSheetOpen] = useState(false);
  const [checked, setChecked] = useState([true, false, false]);
  const active = looks[initialLook];
  const tasks = [
    ["发型", "锁骨长度、偏分、发尾微弯", "给发型师看参考卡"],
    ["妆容", "眉眼提亮，唇色保持低饱和", "预计 8 分钟"],
    ["穿搭", "藏蓝套装 + 象牙白内搭", "现有衣橱可完成"],
  ];
  return (
    <MobileScroll className="app-screen">
      <main className="page plan-page">
        <BrandHeader flow={flow} title="执行清单" step="05 / 05" />
        <section className="plan-hero">
          <img src={active.image} alt={`${active.name}方案`} />
          <div><p className="eyebrow">你选择了</p><h1>{active.name}</h1><p>{active.summary}</p></div>
        </section>

        <div className="task-list">
          {tasks.map(([title, detail, meta], index) => (
            <button
              type="button"
              key={title}
              className={`task-row ${checked[index] ? "checked" : ""}`}
              onClick={() => setChecked((items) => items.map((value, itemIndex) => itemIndex === index ? !value : value))}
            >
              <span className="task-check">{checked[index] ? <CheckIcon /> : null}</span>
              <span className="task-copy"><small>{title}</small><strong>{detail}</strong><em>{meta}</em></span>
              <ChevronRightIcon />
            </button>
          ))}
        </div>

        <button className="share-button" type="button" onClick={() => setSheetOpen(true)}><Share2Icon /> 分享给发型师</button>
        <div className="bottom-action compact">
          <PrimaryButton onClick={() => flow.push(makeFeedbackScreen(active.id))}>完成后回来反馈</PrimaryButton>
          <p>你的反馈会让下一次建议更准确</p>
        </div>

        <BottomSheet open={sheetOpen} onOpenChange={setSheetOpen} title="发型师参考卡" description="保存图片或当面展示">
          <div className="salon-card">
            <img src={active.image} alt="发型参考" />
            <div><strong>锁骨层次发</strong><p>长度：锁骨上下 2 cm</p><p>刘海：6 / 4 偏分</p><p>卷度：发尾自然 C 弯</p></div>
          </div>
          <button className="primary-button sheet-button" type="button" onClick={() => setSheetOpen(false)}><span>保存参考卡</span><CheckIcon /></button>
        </BottomSheet>
      </main>
    </MobileScroll>
  );
}

function makeFeedbackScreen(selected: LookId): FlowScreen {
  return { id: "feedback", render: (flow) => <FeedbackScreen flow={flow} selected={selected} /> };
}

function FeedbackScreen({ flow, selected }: { flow: FlowControls; selected: LookId }) {
  const [rating, setRating] = useState<string | null>(null);
  const [completed, setCompleted] = useState(false);
  const active = looks[selected];
  if (completed) {
    return (
      <MobileScroll className="app-screen">
        <main className="page success-page">
          <div className="success-mark"><CheckIcon /></div>
          <p className="eyebrow">第一次闭环完成</p>
          <h1>我们记住了<br />什么对你有效</h1>
          <p>下一次推荐会优先保留「{active.name}」的发型轮廓与肩线，并根据你的实际反馈继续调整。</p>
          <button className="primary-button" type="button" onClick={() => flow.replace(makePreviewScreen())}><span>回到我的方案</span><ChevronRightIcon /></button>
        </main>
      </MobileScroll>
    );
  }
  return (
    <MobileScroll className="app-screen">
      <main className="page feedback-page">
        <BrandHeader flow={flow} title="实际反馈" />
        <section className="feedback-hero">
          <div className="upload-real"><CameraIcon /><span>上传今天的实拍</span></div>
          <img src={active.image} alt="所选方案参考" />
        </section>
        <section className="feedback-copy">
          <p className="eyebrow">这套方案在现实中怎么样？</p>
          <h1>你的感受比 AI 判断更重要</h1>
        </section>
        <div className="rating-grid">
          {["很像我", "更有精神", "容易做到", "不够自然"].map((item) => (
            <button type="button" key={item} className={rating === item ? "active" : ""} onClick={() => setRating(item)}>{item}</button>
          ))}
        </div>
        <div className="bottom-action compact">
          <PrimaryButton disabled={!rating} onClick={() => setCompleted(true)}>提交反馈</PrimaryButton>
          <button type="button" className="skip-button" onClick={() => setCompleted(true)}>暂时跳过</button>
        </div>
      </main>
    </MobileScroll>
  );
}

function makeSceneScreen(scene = "面试"): FlowScreen {
  return { id: `scene-${scene}`, render: (flow) => <SceneScreen flow={flow} scene={scene} /> };
}

export default function Prototype() {
  const initial = useMemo(() => makeHomeScreen(), []);
  return <FlowStack initial={initial} />;
}
