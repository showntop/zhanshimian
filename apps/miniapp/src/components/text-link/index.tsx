import { Text } from '@tarojs/components'

interface TextLinkProps {
  text: string
  onClick?: () => void
  className?: string
}

export default function TextLink({ text, onClick, className = '' }: TextLinkProps) {
  return (
    <Text className={`text-link pressable ${className}`.trim()} onClick={onClick}>
      {text}
    </Text>
  )
}
