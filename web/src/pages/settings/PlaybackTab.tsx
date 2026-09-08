import { usePrefs } from '../../lib/prefs'
import { useToast } from '../../components/Toast'
import { Field, SectionCard, Toggle } from '../../components/ui'

const QUALITIES = [
  { value: 'auto', label: 'Auto (recommended)' },
  { value: '2160p', label: '4K — up to 2160p' },
  { value: '1080p', label: 'High — up to 1080p' },
  { value: '720p', label: 'Medium — up to 720p' },
  { value: '480p', label: 'Low — up to 480p' },
  { value: '360p', label: 'Data saver — 360p' },
]

// The languages that realistically appear in a personal library. Anything else can
// still be picked per-item in the player.
const LANGUAGES = [
  { value: '', label: 'No preference' },
  { value: 'eng', label: 'English' },
  { value: 'hin', label: 'Hindi' },
  { value: 'spa', label: 'Spanish' },
  { value: 'fra', label: 'French' },
  { value: 'deu', label: 'German' },
  { value: 'ita', label: 'Italian' },
  { value: 'por', label: 'Portuguese' },
  { value: 'jpn', label: 'Japanese' },
  { value: 'kor', label: 'Korean' },
  { value: 'zho', label: 'Chinese' },
  { value: 'rus', label: 'Russian' },
  { value: 'ara', label: 'Arabic' },
  { value: 'tam', label: 'Tamil' },
  { value: 'tel', label: 'Telugu' },
  { value: 'mar', label: 'Marathi' },
  { value: 'ben', label: 'Bengali' },
]

export default function PlaybackTab() {
  const { prefs, update } = usePrefs()
  const toast = useToast()

  async function set<K extends keyof typeof prefs>(key: K, value: (typeof prefs)[K]) {
    try {
      await update({ [key]: value } as any)
    } catch (err: any) {
      toast.error('Could not save that setting', err?.message)
    }
  }

  return (
    <div className="space-y-4">
      <SectionCard
        title="Quality"
        description="Applies to transcoded streams. Files your browser can play directly are always sent untouched."
      >
        <Field
          label="Maximum quality"
          htmlFor="maxQuality"
          hint="Auto lets the player adapt to your connection. Cap it if you are on mobile data or a slow link."
        >
          <select
            id="maxQuality"
            className="select"
            value={prefs.maxQuality}
            onChange={(e) => set('maxQuality', e.target.value)}
          >
            {QUALITIES.map((q) => (
              <option key={q.value} value={q.value}>
                {q.label}
              </option>
            ))}
          </select>
        </Field>
      </SectionCard>

      <SectionCard title="Episodes" description="What happens when an episode finishes.">
        <div className="divide-y divide-ink-700/50">
          <Toggle
            label="Autoplay next episode"
            description="Start the following episode automatically once one ends."
            checked={prefs.autoplayNext}
            onChange={(v) => set('autoplayNext', v)}
          />

          <div className="py-3">
            <Field
              label="Countdown before autoplay"
              htmlFor="countdown"
              hint={`The "up next" card waits ${prefs.autoplayCountdown} seconds before playing.`}
            >
              <input
                id="countdown"
                type="range"
                min={3}
                max={30}
                step={1}
                disabled={!prefs.autoplayNext}
                value={prefs.autoplayCountdown}
                onChange={(e) => set('autoplayCountdown', Number(e.target.value))}
                className="w-full accent-accent disabled:opacity-40"
              />
            </Field>
          </div>

          <Toggle
            label="Show skip intro button"
            description="Offer a skip button during the opening credits when the runtime suggests one."
            checked={prefs.skipIntroEnabled}
            onChange={(v) => set('skipIntroEnabled', v)}
          />
        </div>
      </SectionCard>

      <SectionCard
        title="Language"
        description="Used to preselect tracks when a file offers more than one."
      >
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Preferred audio" htmlFor="audioLang">
            <select
              id="audioLang"
              className="select"
              value={prefs.preferredAudioLang}
              onChange={(e) => set('preferredAudioLang', e.target.value)}
            >
              {LANGUAGES.map((l) => (
                <option key={l.value} value={l.value}>
                  {l.label}
                </option>
              ))}
            </select>
          </Field>

          <Field label="Preferred subtitles" htmlFor="subLang">
            <select
              id="subLang"
              className="select"
              value={prefs.preferredSubLang}
              onChange={(e) => set('preferredSubLang', e.target.value)}
            >
              {LANGUAGES.map((l) => (
                <option key={l.value} value={l.value}>
                  {l.label}
                </option>
              ))}
            </select>
          </Field>
        </div>

        <div className="mt-2 divide-y divide-ink-700/50">
          <Toggle
            label="Subtitles on by default"
            description="Turn subtitles on automatically whenever a matching track exists."
            checked={prefs.subtitlesDefaultOn}
            onChange={(v) => set('subtitlesDefaultOn', v)}
          />
        </div>
      </SectionCard>

      <SectionCard title="Controls" description="How the player responds to you.">
        <Field
          label="Seek step"
          htmlFor="seekStep"
          hint={`The left and right arrow keys jump ${prefs.seekStepSec} seconds.`}
        >
          <select
            id="seekStep"
            className="select"
            value={prefs.seekStepSec}
            onChange={(e) => set('seekStepSec', Number(e.target.value))}
          >
            {[5, 10, 15, 30, 60].map((n) => (
              <option key={n} value={n}>
                {n} seconds
              </option>
            ))}
          </select>
        </Field>
      </SectionCard>
    </div>
  )
}
