export type AgentTabState = {
  tabs: Array<{ name: string, preview: boolean }>
  activeTab: string
}

export function agentTabID(name: string) {
  return `agent:${name}`
}

export function createTabState(pinnedAgents: string[], activeTab: string): AgentTabState {
  return {
    tabs: [...new Set(pinnedAgents)].map((name) => ({ name, preview: false })),
    activeTab,
  }
}

export function previewAgent(state: AgentTabState, name: string): AgentTabState {
  const existing = state.tabs.find((tab) => tab.name === name)
  if (existing) {
    const activeTab = agentTabID(name)
    return state.activeTab === activeTab ? state : { ...state, activeTab }
  }

  const previewIndex = state.tabs.findIndex((tab) => tab.preview)
  const preview = { name, preview: true }
  const tabs = [...state.tabs]
  if (previewIndex === -1) tabs.push(preview)
  else tabs[previewIndex] = preview
  return { tabs, activeTab: agentTabID(name) }
}

export function pinAgent(state: AgentTabState, name: string): AgentTabState {
  const existing = state.tabs.find((tab) => tab.name === name)
  if (!existing) return {
    tabs: [...state.tabs, { name, preview: false }],
    activeTab: agentTabID(name),
  }
  const activeTab = agentTabID(name)
  if (!existing.preview) return state.activeTab === activeTab ? state : { ...state, activeTab }
  return {
    tabs: state.tabs.map((tab) => tab.name === name ? { ...tab, preview: false } : tab),
    activeTab,
  }
}

export function autoPinPreview(state: AgentTabState, name: string): AgentTabState {
  return state.tabs.some((tab) => tab.name === name && tab.preview) ? pinAgent(state, name) : state
}

export function storedPinnedAgents(state: AgentTabState) {
  return state.tabs.filter((tab) => !tab.preview).map((tab) => tab.name)
}
