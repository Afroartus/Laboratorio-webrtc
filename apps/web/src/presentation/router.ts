export type Route = { name: 'home' } | { name: 'call'; callId: string }

export function readRoute(pathname: string): Route {
  const match = /^\/calls\/([^/]+)\/?$/.exec(pathname)
  const callId = match?.[1]
  if (!callId) return { name: 'home' }
  return { name: 'call', callId: decodeURIComponent(callId) }
}

export function navigate(pathname: string): void {
  if (window.location.pathname !== pathname) {
    window.history.pushState(null, '', pathname)
  }
  window.dispatchEvent(new PopStateEvent('popstate'))
}
