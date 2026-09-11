/** 把「调整廓形与比例：卷袖+塞衣角+换鞋」拆成弱分类 + 强动作，避免整句当标题。 */
export function splitAdviceTitle(raw: string): { lead: string; action: string } {
  const text = (raw || '').trim()
  const colon = text.indexOf('：')
  const half = text.indexOf(':')
  const index = colon >= 0 ? colon : half
  const lead = index > 0 ? text.slice(0, index).trim() : ''
  const rest = index > 0 ? text.slice(index + 1).trim() : text
  return {
    lead,
    action: rest.replace(/\s*\+\s*/g, ' + '),
  }
}
