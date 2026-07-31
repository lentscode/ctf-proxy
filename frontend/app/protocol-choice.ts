import type { ScanCandidate } from '../lib/api'

export type ProtocolChoice = {
  selected: boolean
  protocol: 'tcp' | 'http'
  scheme: 'http' | 'https'
}

// defaultProtocolChoice suggests HTTP for the conventional container ports used
// by web services. The operator can always select TCP or change the scheme in
// the review screen before applying a takeover.
export function defaultProtocolChoice(candidate: ScanCandidate): ProtocolChoice {
  switch (candidate.target) {
    case '80':
    case '8000':
    case '8008':
    case '8080':
    case '8081':
    case '8088':
      return { selected: false, protocol: 'http', scheme: 'http' }
    case '443':
    case '8443':
      return { selected: false, protocol: 'http', scheme: 'https' }
    default:
      return { selected: false, protocol: 'tcp', scheme: 'http' }
  }
}
