// 购买判断：单品图上传 + 同步诊断。空态不堆预览卡；结果是一句判断，不套报告卡。
// 结果只活在页面 state；复访由服务端 GET /diagnostics/latest 恢复，不写会话存储。
import { useCallback, useEffect, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { Image, Text, View } from '@tarojs/components'
import { PURCHASE_COPY, type Diagnosis, type DisplayMedia } from '@zsm/core'
import { usePageShell, useShowOnce } from '../../../../hooks/use-page-visibility'
import { peripherals } from '../../../../app/api/peripherals'
import { qualityApi } from '../../../../app/api/quality'
import { mediaUpload } from '../../../../app/api/client'
import { uploadMedia } from '../../../../app/api/media-upload'
import { readLocalImage } from '../../../../features/capture/local-file'
import { handleBillingError } from '../../../../services/billing'
import { splitAdviceTitle } from '../../../../services/advice-title'
import AppHeader from '../../../../components/app-header'
import PrimaryButton from '../../../../components/primary-button'
import SourceImage from '../../../../components/source-image'
import ErrorState from '../../../../components/error-state'
import './index.scss'

export default function Purchase() {
  const [photoPath, setPhotoPath] = useState('')
  const [photoMedia, setPhotoMedia] = useState<DisplayMedia | null>(null)
  const [result, setResult] = useState<Diagnosis | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const resumingRef = useRef(false)
  const freshStartRef = useRef(false)
  const { pageClass, enter } = usePageShell(true, '', 'purchase')

  const applySession = useCallback((item: Diagnosis) => {
    setResult(item)
    if (item.source_media) setPhotoMedia(item.source_media)
  }, [])

  // 复访恢复：服务端 latest 就是唯一事实；本地不再存草稿与结论
  const resume = useCallback(async () => {
    if (resumingRef.current) return
    resumingRef.current = true
    try {
      if (result || freshStartRef.current) return
      const latest = await peripherals.getLatestDiagnosis('purchase')
      if (latest) applySession(latest)
    } catch {
      /* 还没有判断过：保持开始页 */
    } finally {
      resumingRef.current = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [applySession])

  useEffect(() => {
    void resume()
  }, [resume])

  useShowOnce(() => {
    void resume()
  })

  const choosePhoto = () => {
    Taro.chooseMedia({
      count: 1,
      mediaType: ['image'],
      success: (res) => {
        const file = res.tempFiles[0]
        if (file) {
          setPhotoPath(file.tempFilePath)
          setPhotoMedia(null)
          setResult(null)
          freshStartRef.current = true
        }
      },
    })
  }

  const analyze = async (demo = false) => {
    if (busy) return
    setBusy(true)
    setError('')
    try {
      let mediaId: string
      if (demo) {
        const media = await qualityApi.createDemoMedia('body', `purchase-demo:${Date.now()}`)
        mediaId = media.asset_id
        setPhotoMedia(media)
        setPhotoPath('')
      } else {
        if (!photoPath && !photoMedia) {
          Taro.showToast({ title: '先上传商品图', icon: 'none' })
          return
        }
        if (photoPath) {
          const image = await readLocalImage(photoPath)
          mediaId = (await uploadMedia(mediaUpload, image, 'wardrobe')).id
        } else {
          mediaId = photoMedia!.asset_id
        }
      }
      const item = await peripherals.diagnose({ kind: 'purchase', media_id: mediaId })
      applySession(item)
    } catch (e) {
      handleBillingError(e)
      setError((e as Error).message || '判断没有成功，请重试')
    } finally {
      setBusy(false)
    }
  }

  const save = async () => {
    if (!result) return
    try {
      await peripherals.updateDiagnosis(result.id, { saved: true })
      Taro.showToast({ title: PURCHASE_COPY.saved, icon: 'success' })
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '保存没有成功', icon: 'none' })
    }
  }

  const retryFresh = () => {
    freshStartRef.current = true
    setResult(null)
    setError('')
  }

  const shownMedia: DisplayMedia | null = photoMedia ?? result?.source_media ?? null
  const adviceTitle = result?.priority_title || result?.conclusion || PURCHASE_COPY.title
  const { lead: adviceLead, action: adviceAction } = splitAdviceTitle(adviceTitle)
  const adviceBody = result
    ? result.priority_copy || (result.priority_title ? result.conclusion : '')
    : ''
  const keepFindings = (result?.findings ?? []).filter((item) => item.tone === 'positive')
  const liftFindings = (result?.findings ?? []).filter((item) => item.tone !== 'positive')

  return (
    <View className={pageClass}>
      <AppHeader title="购买判断" back />
      <View className={`pk${result ? ' pk--done' : ''}`}>
        <View className={`pk__hero photo-hero photo-hero--bleed ${enter()}`}>
          {shownMedia ? (
            <>
              <SourceImage className="pk__photo-backdrop" media={shownMedia} mode="aspectFill" />
              <SourceImage
                className="pk__hero-img pk__hero-img--fit"
                media={shownMedia}
                mode="aspectFit"
              />
            </>
          ) : photoPath ? (
            <Image
              className="pk__hero-img pk__hero-img--fit"
              src={photoPath}
              mode="aspectFit"
            />
          ) : (
            <View className="pk__upload pressable" onClick={choosePhoto}>
              <SourceImage className="pk__upload-backdrop" reference={{ slug: 'warm', variant: 'full' }} />
              <View className="pk__stage" />
              <SourceImage className="pk__hero-img" reference={{ slug: 'warm', variant: 'full' }} mode="aspectFit" />
              <View className="pk__upload-bar">
                <Text className="pk__upload-bar-plus">＋</Text>
                <Text className="pk__upload-bar-text">{PURCHASE_COPY.uploadTitle}</Text>
              </View>
            </View>
          )}
          {shownMedia || photoPath ? (
            <View className="pk__hero-alt-wrap">
              <Text className="pk__hero-alt pressable" onClick={choosePhoto}>{PURCHASE_COPY.reselect}</Text>
            </View>
          ) : null}
          {busy ? (
            <View className="pk__mask">
              <View className="pk__mask-spin spinner" />
              <Text className="pk__mask-text">{PURCHASE_COPY.busyHint}</Text>
            </View>
          ) : null}
        </View>

        {result ? (
          <View className={`pk__sheet ${enter(1)}`}>
            <View className="pk__advice">
              {adviceLead ? <Text className="pk__advice-lead">{adviceLead}</Text> : null}
              <Text className="pk__advice-title">{adviceAction}</Text>
              {adviceBody ? <Text className="pk__advice-body">{adviceBody}</Text> : null}
            </View>

            {keepFindings.length > 0 ? (
              <View className="pk__keep">
                <Text className="pk__keep-label">{PURCHASE_COPY.findingsKeep}</Text>
                <Text className="pk__keep-text">{keepFindings.map((item) => item.label).join('、')}</Text>
              </View>
            ) : null}

            {liftFindings.length > 0 ? (
              <View className="pk__lifts">
                <Text className="pk__lifts-label">{PURCHASE_COPY.findingsLift}</Text>
                {liftFindings.map((finding) => (
                  <Text key={`${finding.category}-${finding.label}`} className="pk__lift">
                    {finding.label}
                  </Text>
                ))}
              </View>
            ) : null}

            {error ? <ErrorState message={error} onRetry={() => void analyze(false)} /> : null}

            <View className="pk__result-actions">
              <PrimaryButton text={PURCHASE_COPY.save} onClick={() => void save()} />
              <Text className="pk__result-alt pressable" onClick={retryFresh}>{PURCHASE_COPY.again}</Text>
            </View>
          </View>
        ) : (
          <>
            <View className={`pk__hint ${enter(1)}`}>
              <Text className="pk__hint-title">{PURCHASE_COPY.title}</Text>
              <Text className="pk__hint-desc">{PURCHASE_COPY.desc}</Text>
              {!shownMedia && !photoPath ? (
                <Text className="pk__hint-tips">{PURCHASE_COPY.uploadTips.join(' · ')}</Text>
              ) : null}
            </View>
            {error ? <ErrorState message={error} onRetry={() => void analyze(false)} /> : null}
            <View className={`pk__foot ${enter(2)}`}>
              <PrimaryButton
                text={busy ? PURCHASE_COPY.busy : PURCHASE_COPY.start}
                loading={busy}
                disabled={!photoPath && !photoMedia}
                onClick={() => void analyze(false)}
              />
              <View className="pk__foot-row">
                <Text className="pk__foot-alt pressable" onClick={() => void analyze(true)}>{PURCHASE_COPY.demo}</Text>
              </View>
            </View>
          </>
        )}
      </View>
    </View>
  )
}
