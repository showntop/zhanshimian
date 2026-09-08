import { Image, StyleSheet, Text, View, type ImageStyle, type StyleProp } from 'react-native'
import { exampleImage, isBundledAsset, lookImage, userImage } from '@zsm/core'
import { colors, radius } from './theme'

interface LookImageProps {
  src?: string
  slug?: string
  variant?: 'full' | 'portrait' | 'report' | 'hair' | 'plan'
  user?: boolean
  badgeText?: string
  style?: StyleProp<ImageStyle>
}

export function LookImage({ src, slug, variant = 'full', user, badgeText, style }: LookImageProps) {
  let url = ''
  let isExample = false
  if (slug) {
    url = exampleImage(slug, variant)
    isExample = true
  } else if (user) {
    url = userImage(src)
  } else {
    url = lookImage(src)
    isExample = isBundledAsset(src)
  }

  if (!url) {
    return (
      <View style={[styles.empty, style]}>
        <Text style={styles.emptyText}>{user ? '照片暂不可用' : '图片暂不可用'}</Text>
      </View>
    )
  }

  return (
    <View style={[styles.wrap, style]}>
      <Image source={{ uri: url }} style={[styles.img, isExample && styles.soft]} resizeMode="cover" />
      {isExample ? (
        <View style={styles.badge}>
          <Text style={styles.badgeText}>{badgeText || '风格参考'}</Text>
        </View>
      ) : null}
    </View>
  )
}

const styles = StyleSheet.create({
  wrap: { position: 'relative', overflow: 'hidden', borderRadius: radius.md, backgroundColor: colors.mossSoft },
  img: { width: '100%', height: '100%' },
  soft: { opacity: 0.86 },
  empty: {
    alignItems: 'center',
    justifyContent: 'center',
    minHeight: 100,
    backgroundColor: colors.mossSoft,
    borderRadius: radius.md,
  },
  emptyText: { color: colors.ink2, fontSize: 12 },
  badge: {
    position: 'absolute',
    right: 8,
    bottom: 8,
    paddingHorizontal: 8,
    paddingVertical: 3,
    borderRadius: radius.pill,
    backgroundColor: colors.badge,
  },
  badgeText: { color: '#fff', fontSize: 10 },
})
