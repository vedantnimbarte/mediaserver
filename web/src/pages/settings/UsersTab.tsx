import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Shield, Trash2, User as UserIcon, UserPlus } from 'lucide-react'

import { api, type User } from '../../lib/api'
import { useAuth } from '../../lib/auth'
import { useToast } from '../../components/Toast'
import { useConfirm } from '../../components/Modal'
import { Field, SectionCard, Toggle } from '../../components/ui'
import { Skeleton } from '../../components/Skeleton'

export default function UsersTab() {
  const queryClient = useQueryClient()
  const { user: currentUser } = useAuth()
  const toast = useToast()
  const confirm = useConfirm()

  const [adding, setAdding] = useState(false)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [isAdmin, setIsAdmin] = useState(false)

  const { data: users, isLoading } = useQuery({
    queryKey: ['users'],
    queryFn: () => api.get<User[]>('/users'),
  })

  const create = useMutation({
    mutationFn: () => api.post('/users', { username, password, isAdmin }),
    onSuccess: () => {
      toast.success('Account created', `${username} can now sign in.`)
      setAdding(false)
      setUsername('')
      setPassword('')
      setIsAdmin(false)
      queryClient.invalidateQueries({ queryKey: ['users'] })
    },
    onError: (err: any) => toast.error('Could not create the account', err?.message),
  })

  const remove = useMutation({
    mutationFn: (id: string) => api.del(`/users/${id}`),
    onSuccess: () => {
      toast.success('Account removed')
      queryClient.invalidateQueries({ queryKey: ['users'] })
    },
    onError: (err: any) => toast.error('Could not remove the account', err?.message),
  })

  const setRole = useMutation({
    mutationFn: ({ id, admin }: { id: string; admin: boolean }) => api.patch(`/users/${id}`, { isAdmin: admin }),
    onSuccess: () => {
      toast.success('Role updated', 'They will be signed out and need to sign in again.')
      queryClient.invalidateQueries({ queryKey: ['users'] })
    },
    onError: (err: any) => toast.error('Could not change the role', err?.message),
  })

  if (isLoading) {
    return (
      <div className="space-y-3">
        <Skeleton className="h-16 w-full rounded-lg" />
        <Skeleton className="h-16 w-full rounded-lg" />
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <SectionCard title="Accounts" description="Everyone has their own watch history, resume points and preferences.">
        <div className="divide-y divide-ink-700/50">
          {users?.map((u) => {
            const isSelf = u.id === currentUser?.id
            return (
              <div key={u.id} className="flex flex-wrap items-center gap-4 py-3">
                <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-ink-700 text-ink-300">
                  {u.isAdmin ? <Shield className="h-4 w-4" /> : <UserIcon className="h-4 w-4" />}
                </div>

                <div className="min-w-0 flex-1">
                  <p className="font-medium text-ink-100">
                    {u.username}
                    {isSelf && <span className="ml-2 text-xs font-normal text-ink-500">you</span>}
                  </p>
                  <p className="text-xs text-ink-400">
                    {u.isAdmin ? 'Administrator' : 'Standard user'}
                    {u.lastLoginAt && ` · last seen ${new Date(u.lastLoginAt).toLocaleDateString()}`}
                  </p>
                </div>

                <div className="flex items-center gap-2">
                  <button
                    className="btn-sm btn-outline"
                    onClick={async () => {
                      const ok = await confirm({
                        title: u.isAdmin ? `Remove admin from ${u.username}?` : `Make ${u.username} an administrator?`,
                        message: u.isAdmin
                          ? 'They will keep their library access but lose the ability to manage settings.'
                          : 'They will be able to manage libraries, users and every server setting.',
                        confirmLabel: u.isAdmin ? 'Remove admin' : 'Make admin',
                      })
                      if (ok) setRole.mutate({ id: u.id, admin: !u.isAdmin })
                    }}
                  >
                    {u.isAdmin ? 'Remove admin' : 'Make admin'}
                  </button>

                  {!isSelf && (
                    <button
                      className="btn-sm btn-danger"
                      aria-label={`Remove ${u.username}`}
                      onClick={async () => {
                        const ok = await confirm({
                          title: `Remove ${u.username}?`,
                          message: 'Their account and watch history are deleted. This cannot be undone.',
                          confirmLabel: 'Remove account',
                          destructive: true,
                        })
                        if (ok) remove.mutate(u.id)
                      }}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  )}
                </div>
              </div>
            )
          })}
        </div>
      </SectionCard>

      {adding ? (
        <SectionCard title="New account">
          <form
            className="space-y-4"
            onSubmit={(e) => {
              e.preventDefault()
              create.mutate()
            }}
          >
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Username" htmlFor="new-username">
                <input
                  id="new-username"
                  className="input"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  autoComplete="off"
                  required
                />
              </Field>
              <Field label="Password" htmlFor="new-password" hint="At least 8 characters.">
                <input
                  id="new-password"
                  type="password"
                  className="input"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  autoComplete="new-password"
                  required
                />
              </Field>
            </div>

            <Toggle
              label="Administrator"
              description="Can manage libraries, users and every server setting."
              checked={isAdmin}
              onChange={setIsAdmin}
            />

            <div className="flex gap-2">
              <button type="submit" className="btn-primary" disabled={create.isPending}>
                {create.isPending ? 'Creating…' : 'Create account'}
              </button>
              <button type="button" className="btn-outline" onClick={() => setAdding(false)}>
                Cancel
              </button>
            </div>
          </form>
        </SectionCard>
      ) : (
        <button className="btn-primary" onClick={() => setAdding(true)}>
          <UserPlus className="h-4 w-4" />
          Add user
        </button>
      )}
    </div>
  )
}
