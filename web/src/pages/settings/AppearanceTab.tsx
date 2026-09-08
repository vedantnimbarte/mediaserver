import { RotateCcw } from 'lucide-react'

import { usePrefs } from '../../lib/prefs'
import { useToast } from '../../components/Toast'
import { useConfirm } from '../../components/Modal'
import { Field, SectionCard, Toggle } from '../../components/ui'

/** A small palette of accents that all hold up against the near-black shell. */
const ACCENTS = [
  { value: '#E50914', label: 'Netflix red' },
  { value: '#E5A00D', label: 'Amber' },
  { value: '#1DB954', label: 'Green' },
  { value: '#3B82F6', label: 'Blue' },
  { value: '#A855F7', label: 'Purple' },
  { value: '#EC4899', label: 'Pink' },
  { value: '#14B8A6', label: 'Teal' },
  { value: '#F97316', label: 'Orange' },
]

const SIZES = [
  { value: 'small', label: 'Small', hint: 'More per row' },
  { value: 'medium', label: 'Medium', hint: 'Balanced' },
  { value: 'large', label: 'Large', hint: 'Fewer, bigger' },
]

export default function AppearanceTab() {
  const { prefs, update, reset } = usePrefs()
  const toast = useToast()
  const confirm = useConfirm()

  async function set<K extends keyof typeof prefs>(key: K, value: (typeof prefs)[K]) {
    try {
      await update({ [key]: value } as any)
    } catch (err: any) {
      toast.error('Could not save that setting', err?.message)
    }
  }

  async function onReset() {
    const ok = await confirm({
      title: 'Reset your preferences?',
      message: 'Playback and appearance settings go back to their defaults. Your watch history is untouched.',
      confirmLabel: 'Reset',
      destructive: true,
    })
    if (!ok) return
    await reset()
    toast.success('Preferences reset')
  }

  return (
    <div className="space-y-4">
      <SectionCard title="Accent colour" description="Used for buttons, progress bars and focus rings.">
        <div className="flex flex-wrap gap-3">
          {ACCENTS.map((accent) => (
            <button
              key={accent.value}
              onClick={() => set('accentColor', accent.value)}
              title={accent.label}
              aria-label={accent.label}
              aria-pressed={prefs.accentColor.toUpperCase() === accent.value.toUpperCase()}
              className={`h-10 w-10 rounded-full transition-transform hover:scale-110 ${
                prefs.accentColor.toUpperCase() === accent.value.toUpperCase()
                  ? 'ring-2 ring-white ring-offset-2 ring-offset-ink-850'
                  : ''
              }`}
              style={{ backgroundColor: accent.value }}
            />
          ))}

          <label
            className="flex h-10 cursor-pointer items-center gap-2 rounded-full border border-ink-600 px-3
                       text-xs text-ink-300 transition-colors hover:border-ink-400 hover:text-white"
          >
            Custom
            <input
              type="color"
              value={prefs.accentColor}
              onChange={(e) => set('accentColor', e.target.value)}
              className="h-5 w-5 cursor-pointer border-0 bg-transparent p-0"
              aria-label="Custom accent colour"
            />
          </label>
        </div>
      </SectionCard>

      <SectionCard title="Layout" description="How your library is laid out on screen.">
        <Field label="Poster size">
          <div className="grid grid-cols-3 gap-2">
            {SIZES.map((size) => (
              <button
                key={size.value}
                onClick={() => set('posterSize', size.value)}
                className={`rounded-lg border px-3 py-3 text-left transition-colors ${
                  prefs.posterSize === size.value
                    ? 'border-white bg-ink-700'
                    : 'border-ink-600 hover:border-ink-400'
                }`}
              >
                <p className="text-sm font-medium text-white">{size.label}</p>
                <p className="mt-0.5 text-xs text-ink-400">{size.hint}</p>
              </button>
            ))}
          </div>
        </Field>

        <div className="mt-4 divide-y divide-ink-700/50">
          <Toggle
            label="Show titles under posters"
            description="Turn off for a cleaner wall of artwork."
            checked={prefs.showTitles}
            onChange={(v) => set('showTitles', v)}
          />
          <Toggle
            label="Show the billboard on Home"
            description="The large featured banner at the top of the home screen."
            checked={prefs.heroOnHome}
            onChange={(v) => set('heroOnHome', v)}
          />
          <Toggle
            label="Reduce motion"
            description="Disables hover expansion and large transitions throughout the app."
            checked={prefs.reducedMotion}
            onChange={(v) => set('reducedMotion', v)}
          />
        </div>
      </SectionCard>

      <SectionCard title="Reset" description="Restore every playback and appearance setting.">
        <button className="btn-outline" onClick={onReset}>
          <RotateCcw className="h-4 w-4" />
          Reset preferences
        </button>
      </SectionCard>
    </div>
  )
}
