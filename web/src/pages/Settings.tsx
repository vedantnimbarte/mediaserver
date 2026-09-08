import { useSearchParams } from 'react-router-dom'
import { FolderOpen, Palette, PlayCircle, Server, Sliders, Users, Wrench } from 'lucide-react'

import LibrariesTab from './settings/LibrariesTab'
import PlaybackTab from './settings/PlaybackTab'
import AppearanceTab from './settings/AppearanceTab'
import UsersTab from './settings/UsersTab'
import TranscodingTab from './settings/TranscodingTab'
import MaintenanceTab from './settings/MaintenanceTab'
import ServerTab from './settings/ServerTab'

const TABS = [
  { id: 'libraries', label: 'Libraries', icon: <FolderOpen className="h-4 w-4" />, element: <LibrariesTab /> },
  { id: 'playback', label: 'Playback', icon: <PlayCircle className="h-4 w-4" />, element: <PlaybackTab /> },
  { id: 'appearance', label: 'Appearance', icon: <Palette className="h-4 w-4" />, element: <AppearanceTab /> },
  { id: 'users', label: 'Users', icon: <Users className="h-4 w-4" />, element: <UsersTab /> },
  { id: 'transcoding', label: 'Transcoding', icon: <Sliders className="h-4 w-4" />, element: <TranscodingTab /> },
  { id: 'maintenance', label: 'Maintenance', icon: <Wrench className="h-4 w-4" />, element: <MaintenanceTab /> },
  { id: 'server', label: 'Server', icon: <Server className="h-4 w-4" />, element: <ServerTab /> },
]

export default function Settings() {
  // The tab lives in the URL so a particular panel can be linked to and survives a
  // reload, which matters when you are iterating on transcode settings.
  const [params, setParams] = useSearchParams()
  const active = params.get('tab') ?? 'libraries'
  const current = TABS.find((t) => t.id === active) ?? TABS[0]

  return (
    <div className="px-4 py-8 lg:px-12">
      <div className="mx-auto max-w-4xl">
        <h1 className="mb-6 text-2xl font-bold text-white">Settings</h1>

        <div className="mb-6 flex gap-1 overflow-x-auto border-b border-ink-700/60">
          {TABS.map((tab) => (
            <button
              key={tab.id}
              onClick={() => setParams({ tab: tab.id }, { replace: true })}
              className={`-mb-px flex shrink-0 items-center gap-2 border-b-2 px-4 py-2.5 text-sm transition-colors ${
                current.id === tab.id
                  ? 'border-accent font-medium text-white'
                  : 'border-transparent text-ink-400 hover:text-ink-200'
              }`}
            >
              {tab.icon}
              {tab.label}
            </button>
          ))}
        </div>

        {current.element}
      </div>
    </div>
  )
}
