export type ASCIIStyleToken = 'ascii' | 'entropy'

export type ASCIIStyleMode = {
  uplink: ASCIIStyleToken
  downlink: ASCIIStyleToken
}

const normalizeToken = (value: string | null | undefined): ASCIIStyleToken =>
  String(value || '').trim().toLowerCase() === 'ascii' ? 'ascii' : 'entropy'

export const parseAsciiMode = (value: string | null | undefined): ASCIIStyleMode => {
  const raw = String(value || '').trim().toLowerCase()
  if (raw === 'ascii' || raw === 'prefer_ascii') {
    return { uplink: 'ascii', downlink: 'ascii' }
  }
  if (raw.startsWith('up_')) {
    const parts = raw.slice(3).split('_down_')
    if (parts.length === 2) {
      return {
        uplink: normalizeToken(parts[0]),
        downlink: normalizeToken(parts[1]),
      }
    }
  }
  return { uplink: 'entropy', downlink: 'entropy' }
}

export const buildAsciiMode = (mode: ASCIIStyleMode): string => {
  if (mode.uplink === 'ascii' && mode.downlink === 'ascii') {
    return 'prefer_ascii'
  }
  if (mode.uplink === 'entropy' && mode.downlink === 'entropy') {
    return 'prefer_entropy'
  }
  return `up_${mode.uplink}_down_${mode.downlink}`
}

export const canonicalizeAsciiMode = (value: string | null | undefined): string => buildAsciiMode(parseAsciiMode(value))

export const formatAsciiMode = (
  value: string | null | undefined,
  labels: { uplink: string; downlink: string; ascii: string; entropy: string }
): string => {
  const mode = parseAsciiMode(value)
  const uplink = mode.uplink === 'ascii' ? labels.ascii : labels.entropy
  const downlink = mode.downlink === 'ascii' ? labels.ascii : labels.entropy
  return `${labels.uplink} ${uplink} / ${labels.downlink} ${downlink}`
}
