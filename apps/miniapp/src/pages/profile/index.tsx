// 我的（Tab）：账户 hero（头像 / 昵称 / 身份行）、任务中心、权益、形象档案、
// 基本资料、更多、底部隐私签名；弹层：改名 / 修改资料 / 购买次数。
// 视觉恢复自 09-11 旧线（recovery/ui-0911）：moss hero 卡 + 圆头像 + 「修改」、
// 分区卡 + 行/值对、enter(n) 错峰入场。
// 数据纪律（新架构）：/v1/home/bootstrap 聚合 + resourceCache 缓存优先（与首页同一份），
// 昵称/头像来自 getMe，资料卡优先 getMyProfile、回退 boot.profile_summary；
// 切 tab 后台静默对账，失败不拆已渲染内容。角标一律由 SourceImage 按 source_kind 投影。
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import Taro, { useDidShow } from '@tarojs/taro'
import { Input, Text, View } from '@tarojs/components'
import {
  BILLING_COPY,
  DEFAULT_NICKNAME,
  ERROR_COPY,
  IMAGE_BADGE_COPY,
  ME_COPY,
  PROFILE_SETUP_COPY,
  userImage,
  type BillingSummary,
  type DisplayMedia,
  type HomeBootstrap,
  type MeAccount,
  type UserProfile,
} from '@zsm/core'
import { qualityApi } from '../../app/api/quality'
import { peripherals } from '../../app/api/peripherals'
import { mediaUpload } from '../../app/api/client'
import { uploadMedia } from '../../app/api/media-upload'
import { resourceCache, resourceKey } from '../../app/cache/resource-cache'
import { readLocalImage } from '../../features/capture/local-file'
import { reportSourcePhoto } from '../../features/report/model'
import { clearAllLocalState, readStorage, removeStorage, STORAGE_KEYS } from '../../services/storage'
import { usePageShell } from '../../hooks/use-page-visibility'
import AppHeader from '../../components/app-header'
import BottomSheet from '../../components/bottom-sheet'
import CreditSheet from '../../components/credit-sheet'
import ProfileSheet from '../../components/profile-sheet'
import SourceImage from '../../components/source-image'
import PrimaryButton from '../../components/primary-button'
import Skeleton from '../../components/skeleton'
import ErrorState from '../../components/error-state'
import './index.scss'

const HOME_CACHE_KEY = resourceKey('home', 'current')
const IN_FLIGHT = new Set(['accepted', 'running', 'retrying'])

// 公开 OperationRef：core 没有单独命名导出，从 HomeBootstrap 派生（与生成 schema 同源）。
type OperationRef = HomeBootstrap['active_operations'][number]

// 任务中心（简化版）：公开 OperationRef 只有 kind/status——kind 查文案、
// status 分进行中/未完成，没有进度条。execution_feedback 是后台一次性写入，
// 不是用户要等的结果，不进列表。
const TASK_KINDS = new Set<OperationRef['kind']>(['assessment', 'plan_set', 'render', 'body_orbit'])

function visibleOperations(operations: readonly OperationRef[]): OperationRef[] {
  return operations.filter(
    (operation) =>
      TASK_KINDS.has(operation.kind) &&
      (IN_FLIGHT.has(operation.status) || operation.status === 'failed'),
  )
}

/** 点击行进到各自的归属页：失败态的重试也在归属页里完成。 */
function openOperation(operation: OperationRef): void {
  switch (operation.kind) {
    case 'assessment':
      void Taro.navigateTo({
        url:
          `/pages/analysis/index?operation_id=${encodeURIComponent(operation.id)}` +
          `&assessment_id=`,
      })
      return
    case 'plan_set':
    case 'render':
      void Taro.switchTab({ url: '/pages/plans/index' })
      return
    case 'body_orbit':
      void Taro.navigateTo({ url: '/packages/tools/pages/lab/index' })
  }
}

/**
 * avatar_url → user_original DisplayMedia。契约里 MeAccount 只回 URL：
 * asset_id 用 URL 本身顶上（只参与 React key），过期时间放远——
 * URL 新鲜度靠每次 onShow 重新 getMe 对账，客户端不猜签名 TTL。
 */
function avatarMediaFromUrl(url: string): DisplayMedia {
  return {
    asset_id: url,
    url,
    url_expires_at: '2099-12-31T00:00:00.000Z',
    mime_type: 'image/jpeg',
    source_kind: 'user_original',
    display_label: IMAGE_BADGE_COPY.original,
  }
}

export default function Profile() {
  const [boot, setBoot] = useState<HomeBootstrap | null>(
    () => resourceCache.read<HomeBootstrap>(HOME_CACHE_KEY) ?? null,
  )
  const [account, setAccount] = useState<MeAccount | null>(null)
  const [profile, setProfile] = useState<UserProfile | null>(null)
  const [buyOpen, setBuyOpen] = useState(false)
  const [profileOpen, setProfileOpen] = useState(false)
  const [nameOpen, setNameOpen] = useState(false)
  const [nicknameDraft, setNicknameDraft] = useState('')
  const [nameBusy, setNameBusy] = useState(false)
  const [avatarBusy, setAvatarBusy] = useState(false)
  const [loading, setLoading] = useState(!boot)
  const [failed, setFailed] = useState(false)
  const firstShow = useRef(true)
  // 内容上屏后才播入场、播完钉住：切 tab 回来不重播 fade-up（防已渲染图片被藏）
  const { pageClass, enter } = usePageShell(Boolean(boot), 'page--tab', 'profile')

  /** 整体对账：bootstrap 失败才置错误态；me/profile 各自失败保留旧值。返回最新权益供回跳开层。 */
  const load = useCallback(async (background: boolean): Promise<BillingSummary | null> => {
    if (!background) setLoading(true)
    setFailed(false)
    try {
      const [nextBoot, me, myProfile] = await Promise.all([
        resourceCache.revalidate(HOME_CACHE_KEY, () => qualityApi.getHomeBootstrap()),
        peripherals.getMe().catch(() => undefined),
        peripherals.getMyProfile().catch(() => undefined),
      ])
      setBoot(nextBoot)
      if (me) setAccount(me)
      if (myProfile !== undefined) setProfile(myProfile)
      return nextBoot.billing ?? me?.billing ?? null
    } catch {
      // 已有内容时后台刷新失败不拆页，避免切 tab 闪到错误态
      if (!resourceCache.read<HomeBootstrap>(HOME_CACHE_KEY)) setFailed(true)
      return null
    } finally {
      setLoading(false)
    }
  }, [])

  /** 购买回跳：别的页面额度不足时写好标记并 switchTab 过来，这里自动开购买层。 */
  const openCreditSheetIfAsked = useCallback((billing: BillingSummary | null) => {
    if (!readStorage(STORAGE_KEYS.openCreditSheet)) return
    removeStorage(STORAGE_KEYS.openCreditSheet)
    if (billing?.payment_enabled) setBuyOpen(true)
  }, [])

  useEffect(() => {
    void load(false).then(openCreditSheetIfAsked)
  }, [load, openCreditSheetIfAsked])

  // 首次 onShow 跳过（挂载时已拉）；此后每次回 tab 静默对账
  useDidShow(() => {
    if (firstShow.current) {
      firstShow.current = false
      return
    }
    void load(true).then(openCreditSheetIfAsked)
  })

  const report = boot?.report ?? null
  const hasReport = Boolean(report?.id)
  const billing = boot?.billing ?? account?.billing ?? null
  const summary = profile ?? boot?.profile_summary ?? null
  const tasks = visibleOperations(boot?.active_operations ?? [])
  const nickname = account?.nickname?.trim() || DEFAULT_NICKNAME

  // 头像：账户头像优先（URL 先过 userImage 严格校验），回退报告正脸照，再退首字母圆块
  const avatarMedia = useMemo(() => {
    const url = userImage(account?.avatar_url)
    return url ? avatarMediaFromUrl(url) : null
  }, [account?.avatar_url])
  const heroMedia = avatarMedia ?? reportSourcePhoto(report, 'face')

  const pickAvatar = () => {
    if (avatarBusy) return
    Taro.chooseMedia({
      count: 1,
      mediaType: ['image'],
      sourceType: ['album', 'camera'],
      success: (res) => {
        const file = res.tempFiles[0]
        if (!file) return
        setAvatarBusy(true)
        void (async () => {
          try {
            // 上传三步事务：upload-intents → 直传 → complete，与建档页同一套
            const image = await readLocalImage(file.tempFilePath)
            const asset = await uploadMedia(mediaUpload, image, 'face')
            const next = await peripherals.updateMe({ avatar_media_id: asset.id })
            setAccount(next)
            Taro.showToast({ title: PROFILE_SETUP_COPY.saved, icon: 'success' })
          } catch (error) {
            Taro.showToast({
              title: (error as Error).message || ME_COPY.avatarFailed,
              icon: 'none',
            })
          } finally {
            setAvatarBusy(false)
          }
        })()
      },
    })
  }

  const saveNickname = () => {
    const nickname = nicknameDraft.trim()
    if (!nickname) {
      Taro.showToast({ title: ME_COPY.nicknameRequired, icon: 'none' })
      return
    }
    if (nameBusy) return
    setNameBusy(true)
    void (async () => {
      try {
        const next = await peripherals.updateMe({ nickname })
        setAccount(next)
        setNameOpen(false)
        Taro.showToast({ title: PROFILE_SETUP_COPY.saved, icon: 'success' })
      } catch (error) {
        Taro.showToast({ title: (error as Error).message || ME_COPY.saveFailed, icon: 'none' })
      } finally {
        setNameBusy(false)
      }
    })()
  }

  const deleteData = () => {
    Taro.showModal({
      title: ERROR_COPY.deleteConfirmTitle,
      content: ERROR_COPY.deleteConfirmBody,
      confirmColor: '#9B4B45',
      success: (res) => {
        if (!res.confirm) return
        void (async () => {
          try {
            await qualityApi.deleteMyData()
            clearAllLocalState()
            Taro.showToast({ title: PROFILE_SETUP_COPY.deleteDone, icon: 'success' })
            resourceCache.remove(HOME_CACHE_KEY)
            setAccount(null)
            setProfile(null)
            await load(false)
          } catch {
            Taro.showToast({ title: PROFILE_SETUP_COPY.deleteFailed, icon: 'none' })
          }
        })()
      },
    })
  }

  const measuresText =
    summary?.bust_cm || summary?.waist_cm || summary?.hip_cm
      ? `${summary?.bust_cm ?? '—'} / ${summary?.waist_cm ?? '—'} / ${summary?.hip_cm ?? '—'}`
      : ME_COPY.unfilled

  const basicRows = [
    {
      key: 'height',
      label: PROFILE_SETUP_COPY.height,
      value: summary?.height_cm ? `${summary.height_cm} cm` : ME_COPY.unfilled,
    },
    {
      key: 'role',
      label: PROFILE_SETUP_COPY.role,
      value: summary?.role && summary.role !== ME_COPY.unfilled ? summary.role : ME_COPY.unfilled,
    },
    {
      key: 'budget',
      label: PROFILE_SETUP_COPY.budget,
      value:
        summary?.budget && summary.budget !== ME_COPY.unfilled ? summary.budget : ME_COPY.unfilled,
    },
    {
      key: 'weight',
      label: PROFILE_SETUP_COPY.weight,
      value: summary?.weight_kg ? `${summary.weight_kg} kg` : ME_COPY.unfilled,
    },
    { key: 'measures', label: ME_COPY.measurements, value: measuresText },
  ]

  return (
    <View className={pageClass}>
      <AppHeader />
      <View className="me">
        {loading && !boot ? (
          <Skeleton rows={4} />
        ) : failed && !boot ? (
          <ErrorState onRetry={() => void load(false)} />
        ) : (
          <>
            <View className={`me__hero ${enter()}`}>
              <View className="me__avatar-wrap pressable" onClick={pickAvatar}>
                {heroMedia ? (
                  <SourceImage
                    className="me__hero-photo"
                    media={heroMedia}
                    anchor="top"
                    frameAspect={1}
                  />
                ) : (
                  <View className="me__avatar">
                    <Text>{nickname.slice(0, 1)}</Text>
                  </View>
                )}
              </View>
              <View className="me__meta">
                <View className="me__name-row">
                  <Text className="me__nickname">{nickname}</Text>
                  <Text
                    className="me__edit pressable"
                    onClick={() => {
                      setNicknameDraft(nickname)
                      setNameOpen(true)
                    }}
                  >
                    {PROFILE_SETUP_COPY.editAction}
                  </Text>
                </View>
                <Text className="me__identities">
                  {report?.priority_title || report?.priority_copy || ME_COPY.archiveIdentity}
                </Text>
                <Text
                  className="me__hero-link pressable"
                  onClick={() =>
                    void Taro.navigateTo({
                      url: hasReport
                        ? `/pages/report/index?id=${encodeURIComponent(report?.id ?? '')}`
                        : '/pages/capture/index',
                    })
                  }
                >
                  {hasReport ? ME_COPY.viewReportLink : ME_COPY.startArchiveLink}
                </Text>
              </View>
            </View>

            {tasks.length > 0 ? (
              <View className={`me__card ${enter(1)}`}>
                <Text className="me__section">{ME_COPY.tasksTitle}</Text>
                {tasks.map((operation) => {
                  const taskFailed = operation.status === 'failed'
                  return (
                    <View
                      key={operation.id}
                      className="me__row pressable"
                      onClick={() => openOperation(operation)}
                    >
                      <View className="me__row-main">
                        <Text className="me__row-label">
                          {ME_COPY.taskKindLabels[operation.kind]}
                        </Text>
                      </View>
                      <Text
                        className={`me__row-value ${
                          taskFailed ? 'me__row-value--warn' : 'me__row-value--moss'
                        }`}
                      >
                        {taskFailed ? ME_COPY.taskFailed : ME_COPY.taskWorking}
                      </Text>
                    </View>
                  )
                })}
              </View>
            ) : null}

            {billing ? (
              <View className={`me__card ${enter(1)}`}>
                <Text className="me__section">{BILLING_COPY.section}</Text>
                <View className="me__row">
                  <Text className="me__row-label">{BILLING_COPY.remaining}</Text>
                  <Text className="me__row-value me__row-value--moss">
                    {billing.credits} {BILLING_COPY.packUnit}
                  </Text>
                </View>
                <Text className="me__note">{BILLING_COPY.hint}</Text>
                {billing.welcome_analysis_available ? (
                  <Text className="me__note">{BILLING_COPY.welcomeAnalysis}</Text>
                ) : null}
                {billing.welcome_plan_set_available ? (
                  <Text className="me__note">{BILLING_COPY.welcomePlanSet}</Text>
                ) : null}
                <View className="me__row pressable" onClick={() => setBuyOpen(true)}>
                  <Text className="me__row-label">{BILLING_COPY.buyAction}</Text>
                  <Text className="me__row-value">
                    {billing.payment_enabled ? BILLING_COPY.buyNow : BILLING_COPY.paymentUnavailable}
                  </Text>
                </View>
              </View>
            ) : null}

            <View className={`me__card ${enter(1)}`}>
              <Text className="me__section">{ME_COPY.archiveTitle}</Text>
              <View
                className="me__row pressable"
                onClick={() =>
                  void Taro.navigateTo({
                    url: hasReport
                      ? `/pages/report/index?id=${encodeURIComponent(report?.id ?? '')}`
                      : '/pages/report/index',
                  })
                }
              >
                <Text className="me__row-label">{ME_COPY.latestReport}</Text>
                <Text className="me__row-value">
                  {hasReport ? ME_COPY.viewAction : ME_COPY.noArchive}
                </Text>
              </View>
              <View
                className="me__row pressable"
                onClick={() => void Taro.switchTab({ url: '/pages/plans/index' })}
              >
                <Text className="me__row-label">{ME_COPY.myPlans}</Text>
                <Text className="me__row-value">{ME_COPY.viewAction}</Text>
              </View>
              <View
                className="me__row pressable"
                onClick={() => void Taro.navigateTo({ url: '/pages/capture/index?replace=1' })}
              >
                <Text className="me__row-label">{ME_COPY.updateArchive}</Text>
                <Text className="me__row-value">{ME_COPY.updateArchiveHint}</Text>
              </View>
            </View>

            <View className={`me__card ${enter(2)}`}>
              <Text className="me__section">{ME_COPY.basicsTitle}</Text>
              {basicRows.map((row) => (
                <View
                  key={row.key}
                  className="me__row pressable"
                  onClick={() => setProfileOpen(true)}
                >
                  <Text className="me__row-label">{row.label}</Text>
                  <Text className="me__row-value">{row.value}</Text>
                </View>
              ))}
            </View>

            <View className={`me__card ${enter(2)}`}>
              <Text className="me__section">{ME_COPY.moreTitle}</Text>
              <View
                className="me__row pressable"
                onClick={() => void Taro.navigateTo({ url: '/packages/tools/pages/lab/index' })}
              >
                <Text className="me__row-label">{ME_COPY.labEntry}</Text>
                <Text className="me__row-value">{ME_COPY.labEntryHint}</Text>
              </View>
              <View
                className="me__row pressable"
                onClick={() => void Taro.navigateTo({ url: '/packages/life/pages/wardrobe/index' })}
              >
                <Text className="me__row-label">{ME_COPY.wardrobeEntry}</Text>
                <Text className="me__row-value">{ME_COPY.wardrobeEntryHint}</Text>
              </View>
              <View className="me__row pressable" onClick={deleteData}>
                <Text className="me__row-label me__row-label--danger">
                  {PROFILE_SETUP_COPY.deleteAction}
                </Text>
                <Text className="me__row-value">{ME_COPY.deleteAllHint}</Text>
              </View>
            </View>

            <Text className={`me__privacy ${enter(3)}`}>{ME_COPY.privacyNote}</Text>
          </>
        )}
      </View>

      <BottomSheet
        open={nameOpen}
        title={PROFILE_SETUP_COPY.editName}
        onClose={() => setNameOpen(false)}
      >
        <View className="me__name-sheet">
          <Input
            className="me__name-input"
            maxlength={20}
            value={nicknameDraft}
            onInput={(event) => setNicknameDraft(event.detail.value)}
          />
          <PrimaryButton text={PROFILE_SETUP_COPY.save} loading={nameBusy} onClick={saveNickname} />
          <Text
            className="me__hero-link pressable"
            onClick={() => {
              setNameOpen(false)
              pickAvatar()
            }}
          >
            {PROFILE_SETUP_COPY.changePhoto} ›
          </Text>
        </View>
      </BottomSheet>
      <BottomSheet
        open={profileOpen}
        title={PROFILE_SETUP_COPY.editTitle}
        description={PROFILE_SETUP_COPY.editBody}
        tall
        onClose={() => setProfileOpen(false)}
      >
        <ProfileSheet
          profile={summary}
          onSaved={(next) => {
            setProfile(next)
            setProfileOpen(false)
          }}
        />
      </BottomSheet>
      <BottomSheet
        open={buyOpen}
        title={BILLING_COPY.buyAction}
        description={BILLING_COPY.insufficientBody}
        onClose={() => setBuyOpen(false)}
      >
        <CreditSheet
          billing={billing}
          onPurchased={() => {
            setBuyOpen(false)
            void load(true)
          }}
        />
      </BottomSheet>
    </View>
  )
}
