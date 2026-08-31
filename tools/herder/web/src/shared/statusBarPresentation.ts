export type HealthTick = {
  label: 'herdr' | 'hcom' | 'SSE'
  healthy: boolean
  title: string
}

type HealthInput = {
  problems: Record<string, string>
  substrateProof: { herdr: boolean, hcom: boolean }
  lastEventLabel: string
}

function substrateTick(
  label: 'herdr' | 'hcom',
  problems: Record<string, string>,
  proven: boolean,
): HealthTick {
  const display = label === 'herdr' ? 'Herdr' : 'hcom'
  if (problems[label]) return { label, healthy: false, title: `${display} unavailable — ${problems[label]}` }
  if (problems.stream && problems.stream !== 'Connecting to live fleet…') {
    return { label, healthy: false, title: `${display} health unavailable while SSE reconnects.` }
  }
  if (!proven) {
    return {
      label,
      healthy: false,
      title: label === 'herdr'
        ? 'Herdr not yet proven — waiting for the first fleet snapshot.'
        : 'hcom not yet proven — waiting for bus health.',
    }
  }
  return {
    label,
    healthy: true,
    title: label === 'herdr'
      ? 'Herdr reachable — latest fleet snapshot succeeded.'
      : 'hcom healthy — roster and bus event subscription are reachable.',
  }
}

export function statusBarHealth({ problems, substrateProof, lastEventLabel }: HealthInput): HealthTick[] {
  return [
    substrateTick('herdr', problems, substrateProof.herdr),
    substrateTick('hcom', problems, substrateProof.hcom),
    problems.stream
      ? { label: 'SSE', healthy: false, title: problems.stream }
      : { label: 'SSE', healthy: true, title: `SSE connected — last activity ${lastEventLabel}.` },
  ]
}
