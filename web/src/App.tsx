import { useEffect, useState } from 'react'

interface Health {
  status: string
  app: string
}

function App() {
  const [health, setHealth] = useState<Health | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    fetch('/api/health')
      .then((r) => (r.ok ? r.json() : Promise.reject('health check failed')))
      .then(setHealth)
      .catch((e) => setError(String(e)))
  }, [])

  return (
    <main style={{ fontFamily: 'system-ui, sans-serif', padding: '2rem' }}>
      <h1>Azure DevOps Permissions Visualiser</h1>
      <p>Product skeleton is up.</p>
      {error && <p style={{ color: 'red' }}>Backend unreachable: {error}</p>}
      {health && (
        <p>
          Backend status: <strong>{health.status}</strong> ({health.app})
        </p>
      )}
    </main>
  )
}

export default App