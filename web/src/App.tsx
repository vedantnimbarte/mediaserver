import { Navigate, Route, Routes } from 'react-router-dom'

import Layout from './components/Layout'
import { ErrorBox, Spinner } from './components/ui'
import { useAuth } from './lib/auth'

import Login from './pages/Login'
import Home from './pages/Home'
import Movies from './pages/Movies'
import MovieDetail from './pages/MovieDetail'
import Shows from './pages/Shows'
import ShowDetail from './pages/ShowDetail'
import MusicLibrary from './pages/Music'
import Photos from './pages/Photos'
import SearchResults from './pages/Search'
import Player from './pages/Player'
import Settings from './pages/Settings'

export default function App() {
  const { user, serverInfo, loading } = useAuth()

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center">
        <Spinner label="Connecting…" />
      </div>
    )
  }

  if (!serverInfo) {
    return (
      <div className="flex h-full items-center justify-center p-6">
        <ErrorBox
          title="Cannot reach the server"
          message="The media server is not responding. Check that it is running, then reload this page."
          action={
            <button className="btn-primary" onClick={() => window.location.reload()}>
              Reload
            </button>
          }
        />
      </div>
    )
  }

  if (!user) {
    return <Login />
  }

  return (
    <Routes>
      {/* The player is deliberately outside the shell: it takes the whole screen. */}
      <Route path="/play/:mediaId" element={<Player />} />

      <Route
        path="*"
        element={
          <Layout>
            <Routes>
              <Route path="/" element={<Home />} />
              <Route path="/movies" element={<Movies />} />
              <Route path="/movies/:id" element={<MovieDetail />} />
              <Route path="/shows" element={<Shows />} />
              <Route path="/shows/:id" element={<ShowDetail />} />
              <Route path="/music" element={<MusicLibrary />} />
              <Route path="/photos" element={<Photos />} />
              <Route path="/search" element={<SearchResults />} />
              <Route path="/settings" element={user.isAdmin ? <Settings /> : <Navigate to="/" replace />} />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
          </Layout>
        }
      />
    </Routes>
  )
}
