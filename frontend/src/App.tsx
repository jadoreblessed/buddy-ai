import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import './App.css'

type Status = 'done' | 'current' | 'pending' | 'locked'

type Step = {
  id: string
  title: string
  detail: string
  actionType: 'none' | 'request_access'
  status: Status
  resource?: string
}

type AccessRequest = {
  id: string
  internId: string
  planId: string
  stepId: string
  resource: string
  status: 'pending' | 'approved'
  createdAt: string
  approvedAt?: string
}

type Mentor = { id: string; name: string; channel: string }
type Plan = {
  planId: string
  intern: string
  position: 'backend' | 'frontend' | 'analyst'
  role: string
  team: string
  progress: number
  steps: Step[]
  request: AccessRequest | null
  mentor: Mentor
}
type Page = 'plan' | 'buddy' | 'request' | 'materials' | 'absence' | 'mentor'
type AbsenceRequest = {
  id: string
  internId: string
  date: string
  status: 'pending' | 'approved'
  createdAt: string
  approvedAt?: string
}
type MentorMessage = {
  id: string
  internId: string
  mentorId: string
  text: string
  status: 'sent'
  createdAt: string
}
type Material = { id: string; title: string; content: string; url?: string }

type BuddySource = { id: string; title: string; excerpt: string }
type BuddyTurn = {
  question: string
  answer?: string
  sources?: BuddySource[]
  model?: string
  toolCall?: string
  error?: string
  suggestedAction?: 'message_mentor'
}

// In Vite development, all API calls use its same-origin proxy.
const API = import.meta.env.DEV ? '' : (import.meta.env.VITE_API_URL || '')

const statusText: Record<Status, string> = {
  done: 'Выполнено',
  current: 'Сейчас',
  pending: 'Ожидает согласования',
  locked: 'Следующий шаг',
}

async function apiResult<T>(response: Response): Promise<T> {
  const data = await response.json().catch(() => null) as (T & { error?: string }) | null
  if (!response.ok) throw new Error(data?.error || `Сервис недоступен (HTTP ${response.status}). Попробуй ещё раз.`)
  if (!data) throw new Error('Сервер вернул некорректный ответ. Попробуй ещё раз.')
  return data
}

function ConfirmDialog({ title, children, confirmLabel, busy, onCancel, onConfirm }: {
  title: string
  children: ReactNode
  confirmLabel: string
  busy: boolean
  onCancel: () => void
  onConfirm: () => void
}) {
  const dialog = useRef<HTMLDialogElement>(null)
  useEffect(() => {
    const element = dialog.current
    element?.showModal()
    return () => element?.close()
  }, [])
  return (
    <dialog ref={dialog} className="scenario-dialog" aria-labelledby="scenario-confirm-title"
      onCancel={(event) => { if (busy) event.preventDefault(); else onCancel() }}>
      <h2 id="scenario-confirm-title">{title}</h2>
      {children}
      <div className="confirm-actions">
        <button type="button" className="cancel-button" disabled={busy} onClick={onCancel}>Отмена</button>
        <button type="button" className="action-button" disabled={busy} onClick={onConfirm}>{confirmLabel}</button>
      </div>
    </dialog>
  )
}

function App() {
  const [plan, setPlan] = useState<Plan | null>(null)
  const [page, setPage] = useState<Page>('plan')
  const [busy, setBusy] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [confirmStepId, setConfirmStepId] = useState<string | null>(null)
  const [question, setQuestion] = useState('')
  const [turns, setTurns] = useState<BuddyTurn[]>([])
  const [answering, setAnswering] = useState(false)
  const [materials, setMaterials] = useState<Material[] | null>(null)
  const [materialsKey, setMaterialsKey] = useState('')
  const [materialsLoading, setMaterialsLoading] = useState(false)
  const [materialsError, setMaterialsError] = useState('')
  const [materialsAttempt, setMaterialsAttempt] = useState(0)
  const [selectedMaterialId, setSelectedMaterialId] = useState<string | null>(null)
  const [absenceDate, setAbsenceDate] = useState(() => new Date().toISOString().slice(0, 10))
  const [absences, setAbsences] = useState<AbsenceRequest[] | null>(null)
  const [absenceLoading, setAbsenceLoading] = useState(false)
  const [absenceError, setAbsenceError] = useState('')
  const [absenceAttempt, setAbsenceAttempt] = useState(0)
  const [absenceBusy, setAbsenceBusy] = useState(false)
  const [absenceConfirm, setAbsenceConfirm] = useState<string | null>(null)
  const [absenceReceipt, setAbsenceReceipt] = useState<AbsenceRequest | null>(null)
  const [mentorDraft, setMentorDraft] = useState('')
  const [messages, setMessages] = useState<MentorMessage[] | null>(null)
  const [messagesKey, setMessagesKey] = useState('')
  const [messagesLoading, setMessagesLoading] = useState(false)
  const [messagesError, setMessagesError] = useState('')
  const [messagesAttempt, setMessagesAttempt] = useState(0)
  const [messageBusy, setMessageBusy] = useState(false)
  const [messageConfirm, setMessageConfirm] = useState<string | null>(null)
  const [messageReceipt, setMessageReceipt] = useState<MentorMessage | null>(null)

  const loadPlan = useCallback(async () => {
    setLoading(true)
    try {
      const response = await fetch(`${API}/api/plan`)
      if (!response.ok) {
        const body = await response.text()
        let message = `Не удалось загрузить план (${response.status})`
        try {
          const data = JSON.parse(body) as { error?: string }
          message = data.error || message
        } catch {
          // A proxy can return HTML; never render its entire response in the UI.
        }
        throw new Error(message)
      }

      setPlan((await response.json()) as Plan)
      setError('')
    } catch (cause) {
      setPlan(null)
      setError(cause instanceof Error && cause.message !== 'Failed to fetch'
        ? cause.message
        : 'Нет связи с Go-сервером. Запусти go run ./backend.')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadPlan()
  }, [loadPlan])
  const planKey = plan ? `${plan.planId}:${plan.position}` : ''
  useEffect(() => {
    if (!planKey) return
    const controller = new AbortController()
    setMaterialsLoading(true)
    setMaterialsError('')
    void (async () => {
      try {
        const response = await fetch(`${API}/api/materials`, { signal: controller.signal })
        if (!response.ok) throw new Error(`Не удалось загрузить материалы (${response.status})`)
        const data = await response.json() as { materials?: Material[] }
        if (!Array.isArray(data.materials)) throw new Error('Сервер вернул неполный список материалов.')
        if (!controller.signal.aborted) {
          setMaterials(data.materials)
          setMaterialsKey(planKey)
        }
      } catch (cause) {
        if (!controller.signal.aborted) {
          setMaterialsError(cause instanceof Error && cause.message !== 'Failed to fetch'
            ? cause.message : 'Нет связи с сервером материалов.')
        }
      } finally {
        if (!controller.signal.aborted) setMaterialsLoading(false)
      }
    })()
    return () => controller.abort()
  }, [planKey, materialsAttempt])
  const mentorKey = plan ? `${planKey}:${plan.mentor?.id ?? ''}` : ''
  useEffect(() => {
    if (page !== 'absence' || !planKey) return
    const controller = new AbortController()
    setAbsenceLoading(true)
    setAbsenceError('')
    void (async () => {
      try {
        const data = await apiResult<{ requests: AbsenceRequest[] }>(
          await fetch(`${API}/api/absences`, { headers: { 'X-Demo-User': 'intern-1' }, signal: controller.signal }),
        )
        if (!Array.isArray(data.requests)) throw new Error('Сервер вернул неполный список отгулов.')
        if (!controller.signal.aborted) setAbsences(data.requests)
      } catch (cause) {
        if (!controller.signal.aborted) setAbsenceError(cause instanceof Error && cause.message !== 'Failed to fetch'
          ? cause.message : 'Нет связи с сервисом отгулов. Попробуй ещё раз.')
      } finally {
        if (!controller.signal.aborted) setAbsenceLoading(false)
      }
    })()
    return () => controller.abort()
  }, [page, planKey, absenceAttempt])

  useEffect(() => {
    if (page !== 'mentor' || !mentorKey || !plan?.mentor?.id) return
    const controller = new AbortController()
    setMessagesLoading(true)
    setMessagesError('')
    void (async () => {
      try {
        const data = await apiResult<{ mentor: Mentor; messages: MentorMessage[] }>(
          await fetch(`${API}/api/mentor/messages`, { headers: { 'X-Demo-User': 'intern-1' }, signal: controller.signal }),
        )
        if (!Array.isArray(data.messages) || data.mentor?.id !== plan.mentor.id) {
          throw new Error('Сервер вернул данные другого наставника или неполную историю.')
        }
        if (!controller.signal.aborted) {
          setMessages(data.messages)
          setMessagesKey(mentorKey)
        }
      } catch (cause) {
        if (!controller.signal.aborted) setMessagesError(cause instanceof Error && cause.message !== 'Failed to fetch'
          ? cause.message : 'Нет связи с демо-мессенджером. Попробуй ещё раз.')
      } finally {
        if (!controller.signal.aborted) setMessagesLoading(false)
      }
    })()
    return () => controller.abort()
  }, [page, mentorKey, messagesAttempt])

  useEffect(() => {
    if (!planKey) return
    const firstTask = plan?.steps.find((step) => step.actionType === 'none' && step.status !== 'done')
    setMentorDraft(`Здравствуйте! Подскажите, пожалуйста, с чего начать ${firstTask ? `задачу «${firstTask.title}»` : 'первую задачу'} по моему плану адаптации?`)
    setMessageReceipt(null)
    setMessages(null)
    setMessagesKey('')
  }, [mentorKey])

  function openMaterial(id: string) {
    setSelectedMaterialId(id)
    setPage('materials')
    window.scrollTo(0, 0)
  }
  function renderAnswer(turn: BuddyTurn) {
    let answer = turn.answer ?? ''
    const listStart = answer.lastIndexOf('\nИсточники:')
    if (listStart >= 0) {
      const lines = answer.slice(listStart + '\nИсточники:'.length).trim().split('\n').filter(Boolean)
      if (lines.length > 0 && lines.every((line) => {
        const citations = [...line.matchAll(/\[([^\[\]\n]+)\]/g)]
        return citations.length > 0 && citations.every((match) => turn.sources?.some((source) => source.id === match[1]))
      })) {
        answer = answer.slice(0, listStart).trimEnd()
      }
    }

    const hasCitation = turn.sources?.some((source) => answer.includes(`[${source.id}]`))
    const content = answer.split(/(\[[^\[\]\n]+\]|\*\*[^*\n]+\*\*|`[^`\n]+`)/g).map((part, index) => {
      if (part.startsWith('**') && part.endsWith('**')) return <strong key={index}>{part.slice(2, -2)}</strong>
      if (part.startsWith('`') && part.endsWith('`')) return <code key={index}>{part.slice(1, -1)}</code>
      const source = part.startsWith('[') && part.endsWith(']')
        ? turn.sources?.find((item) => item.id === part.slice(1, -1))
        : undefined
      if (!source) return part

      const available = materialsKey === planKey && materials?.some((item) => item.id === source.id && !item.url)
      return (
        <button
          type="button"
          className="inline-source"
          key={index}
          disabled={!available}
          aria-label={`Открыть источник: ${source.title}`}
          title={available ? source.excerpt : 'Материал недоступен в текущем плане'}
          onClick={() => openMaterial(source.id)}
        >
          <span>{source.title}</span>
          <span aria-hidden="true">↗</span>
        </button>
      )
    })

    return (
      <>
        <p>{content}</p>
        {turn.sources && turn.sources.length > 0 && !hasCitation && (
          <div className="buddy-sources">
            <strong>Материалы по теме</strong>
            <div className="source-list">
              {turn.sources.map((source) => (
                <button
                  type="button"
                  className="inline-source"
                  key={source.id}
                  disabled={materialsKey !== planKey || !materials?.some((item) => item.id === source.id && !item.url)}
                  aria-label={`Открыть источник: ${source.title}`}
                  onClick={() => openMaterial(source.id)}
                >
                  <span>{source.title}</span>
                  <span aria-hidden="true">↗</span>
                </button>
              ))}
            </div>
          </div>
        )}
      </>
    )
  }


  async function post(path: string, user: string) {
    setBusy(true)
    setError('')

    try {
      const response = await fetch(`${API}${path}`, {
        method: 'POST',
        headers: { 'X-Demo-User': user },
      })

      if (!response.ok) throw new Error('Не удалось выполнить действие')

      await loadPlan()
    } catch {
      setError('Действие не выполнено. Проверь, запущен ли Go-сервер.')
    } finally {
      setBusy(false)
    }
  }

  async function createAbsence(date: string) {
    if (absenceBusy) return
    setAbsenceBusy(true)
    setAbsenceError('')
    setAbsenceReceipt(null)
    try {
      const data = await apiResult<{ request: AbsenceRequest }>(await fetch(`${API}/api/absences`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-Demo-User': 'intern-1' },
        body: JSON.stringify({ date }),
      }))
      if (!data.request?.id || data.request.internId !== 'intern-1' || data.request.date !== date || !['pending', 'approved'].includes(data.request.status)) {
        throw new Error('Сервер вернул неполную заявку на отгул.')
      }
      setAbsences((previous) => [data.request, ...(previous ?? []).filter((item) => item.id !== data.request.id)])
      setAbsenceReceipt(data.request)
    } catch (cause) {
      setAbsenceError(cause instanceof Error && cause.message !== 'Failed to fetch'
        ? cause.message : 'Не удалось создать отгул: нет связи с сервисом. Попробуй ещё раз.')
    } finally {
      setAbsenceBusy(false)
    }
  }

  async function approveAbsence(id: string) {
    if (absenceBusy) return
    setAbsenceBusy(true)
    setAbsenceError('')
    try {
      const data = await apiResult<{ request: AbsenceRequest }>(await fetch(
        `${API}/api/absences/${encodeURIComponent(id)}/approve`,
        { method: 'POST', headers: { 'X-Demo-User': plan?.mentor.id ?? '' } },
      ))
      if (data.request?.id !== id || data.request.status !== 'approved') {
        throw new Error('Сервер не подтвердил согласование отгула.')
      }
      setAbsences((previous) => previous?.map((item) => item.id === id ? data.request : item) ?? null)
      setAbsenceReceipt((previous) => previous?.id === id ? data.request : previous)
    } catch (cause) {
      setAbsenceError(cause instanceof Error && cause.message !== 'Failed to fetch'
        ? cause.message : 'Не удалось согласовать отгул: нет связи с сервисом. Попробуй ещё раз.')
    } finally {
      setAbsenceBusy(false)
    }
  }

  async function sendMentorMessage(text: string, mentor: Mentor) {
    if (messageBusy) return
    setMessageBusy(true)
    setMessagesError('')
    setMessageReceipt(null)
    try {
      const data = await apiResult<{ message: MentorMessage }>(await fetch(`${API}/api/mentor/messages`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-Demo-User': 'intern-1' },
        body: JSON.stringify({ text }),
      }))
      if (!data.message?.id || data.message.internId !== 'intern-1' || data.message.mentorId !== mentor.id || data.message.status !== 'sent' || data.message.text !== text) {
        throw new Error('Сервер не подтвердил доставку сообщения наставнику.')
      }
      setMessages((previous) => [...(previous ?? []), data.message])
      setMessageReceipt(data.message)
      setMentorDraft('')
    } catch (cause) {
      setMessagesError(cause instanceof Error && cause.message !== 'Failed to fetch'
        ? cause.message : 'Не удалось отправить сообщение: нет связи с демо-мессенджером. Попробуй ещё раз.')
    } finally {
      setMessageBusy(false)
    }
  }

  async function askBuddy(value: string, retryIndex?: number) {
    const trimmed = value.trim()
    if (!trimmed || answering) return

    const index = retryIndex ?? turns.length
    setAnswering(true)
    setQuestion('')
    if (retryIndex === undefined) {
      setTurns((previous) => [...previous, { question: trimmed }])
    } else {
      setTurns((previous) => previous.map((turn, position) =>
        position === index ? { question: turn.question } : turn,
      ))
    }

    try {
      const response = await fetch(`${API}/api/buddy/answer`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ question: trimmed }),
      })
      const data = await response.json().catch(() => {
        throw new Error(`Сервер вернул не JSON (HTTP ${response.status}). Проверь адрес страницы и Vite proxy.`)
      }) as {
        answer?: string
        sources?: BuddySource[]
        error?: string
        model?: string
        toolCall?: string
        suggestedAction?: 'message_mentor'
      }
      if (!response.ok) throw new Error(data.error || `Не удалось получить ответ (${response.status})`)
      if (typeof data.answer !== 'string' || !Array.isArray(data.sources)) {
        throw new Error('Сервер вернул неполный ответ. Попробуй ещё раз.')
      }
      setTurns((previous) => previous.map((turn, position) =>
        position === index ? { question: trimmed, answer: data.answer, sources: data.sources, model: data.model, toolCall: data.toolCall, suggestedAction: data.suggestedAction } : turn,
      ))
    } catch (cause) {
      const message = cause instanceof Error && cause.message !== 'Failed to fetch' && !(cause instanceof SyntaxError)
        ? cause.message
        : 'Нет связи с помощником. Проверь, запущен ли сервер, и попробуй ещё раз.'
      setTurns((previous) => previous.map((turn, position) =>
        position === index ? { question: trimmed, error: message } : turn,
      ))
    } finally {
      setAnswering(false)
    }
  }

  const request = plan && plan.request?.planId === plan.planId ? plan.request : null
  const currentStep = plan?.steps.find((step) => step.status === 'current' || step.status === 'pending')
  const accessStep = plan?.steps.find((step) => step.actionType === 'request_access')
  const requestStep = plan?.steps.find((step) => step.id === request?.stepId)
  const confirmStep = plan?.steps.find((step) => step.id === confirmStepId && step.status === 'current' && step.actionType === 'request_access')
  const total = plan?.steps.length ?? 0

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <button className="brand" onClick={() => setPage('plan')} aria-label="На главную">
          <span className="brand-mark">
            <i /><i /><i /><i />
          </span>
          <span>
            рядом<span className="brand-dot">.</span>
          </span>
        </button>

        <div className="sidebar-middle">
          <p className="sidebar-caption">Твой первый день</p>

          <nav className="navigation" aria-label="Разделы">
            <button
              className={page === 'plan' ? 'active' : ''}
              onClick={() => setPage('plan')}
            >
              <span>01</span> План адаптации
            </button>

            <button
              className={page === 'buddy' ? 'active' : ''}
              onClick={() => setPage('buddy')}
            >
              <span className="nav-star">✳</span> Спросить AI-buddy
            </button>

            <button
              className={page === 'request' ? 'active' : ''}
              onClick={() => setPage('request')}
            >
              <span>↗</span> Моя заявка
            </button>

            <button type="button" className={page === 'absence' ? 'active' : ''}
              aria-current={page === 'absence' ? 'page' : undefined}
              onClick={() => setPage('absence')}>
              <span aria-hidden="true">◷</span> Отгул на день
            </button>

            <button type="button" className={page === 'mentor' ? 'active' : ''}
              aria-current={page === 'mentor' ? 'page' : undefined}
              onClick={() => setPage('mentor')}>
              <span aria-hidden="true">✉</span> Написать наставнику
            </button>

            <button
              className={page === 'materials' ? 'active' : ''}
              onClick={() => setPage('materials')}
            >
              <span>▤</span> Материалы
            </button>
          </nav>

          <div className="sidebar-note">
            <button type="button" onClick={() => setPage('materials')}>Открыть инструкции и материалы ↗</button>
          </div>
        </div>

        {plan && (
          <div className="profile">
            <span className="avatar">{plan.intern.charAt(0)}</span>
            <span>
              <strong>{plan.intern}</strong>
              <small>{plan.role} · {plan.team}</small>
            </span>
          </div>
        )}
      </aside>

      <div className="workspace">
        <header className="topbar">
          <span>Стажировка / Первый день</span>
          <span>
            Демо-прототип <b>•</b>
          </span>
        </header>

        <main className="content">
          {error && (
            <div className="error-banner" role="alert">
              <span>{error}</span>
              {!plan && <button className="text-button" onClick={() => void loadPlan()} disabled={loading}>Повторить ↗</button>}
            </div>
          )}
          {loading && !plan && <p role="status">Загружаем назначенный план…</p>}

          {plan && page === 'plan' && (
            <>
              <section className="hero">
                <div className="hero-copy">
                  <h1>
                    <mark>Первый день.</mark>
                    <br />
                    Давай по шагам.
                  </h1>
                  <p className="hero-description">
                    Твой маршрут на позиции {plan.position}: {plan.role} в команде
                    «{plan.team}». Неясно, что делать дальше? Открой подсказки помощника.
                  </p>
                </div>

                <div className="sticky-note" aria-hidden="true">
                  <span>
                    твоя точка
                    <br />
                    старта
                  </span>
                  <b>01—{String(total).padStart(2, '0')}</b>
                  <i>✳</i>
                </div>
              </section>

              <section className="plan-section">
                <div className="section-heading">
                  <div>
                    <p className="eyebrow">01 / Твой маршрут</p>
                    <h2>План первого дня</h2>
                    <p className="section-subtitle">
                      {currentStep
                        ? `${currentStep.status === 'pending' ? 'Ожидает согласования' : 'Сейчас'}: ${currentStep.title}.`
                        : 'Все шаги плана выполнены.'}
                    </p>
                  </div>

                  <strong className="progress-label">
                    {plan.progress} из {total}
                  </strong>
                </div>

                <div
                  className="progress-track"
                  role="progressbar"
                  aria-valuenow={plan.progress}
                  aria-valuemin={0}
                  aria-valuemax={total}
                >
                  <span
                    style={{
                      width: `${total ? (plan.progress / total) * 100 : 0}%`,
                    }}
                  />
                </div>

                <div className="steps">
                  {plan.steps.map((step, index) => (
                    <article className={`step step-${step.status}`} key={step.id}>
                      <span className="step-number">
                        {String(index + 1).padStart(2, '0')}
                      </span>

                      <div className="step-body">
                        <h3>{step.title}</h3>
                        <p>{step.detail}</p>

                        {step.actionType === 'request_access' && step.status === 'current' && (
                          <button
                            className="action-button"
                            disabled={busy || !step.resource}
                            onClick={() => setConfirmStepId(step.id)}
                          >
                            Запросить доступ ↗
                          </button>
                        )}

                        {step.actionType === 'request_access' && step.status === 'pending' && request?.stepId === step.id && (
                          <button
                            className="text-button"
                            onClick={() => setPage('request')}
                          >
                            Заявка {request.id} · посмотреть статус ↗
                          </button>
                        )}
                      </div>

                      <span className="step-status">
                        {statusText[step.status]}
                      </span>
                    </article>
                  ))}
                </div>
              </section>
            </>
          )}

          {plan && page === 'request' && (
            <section className="inner-page">
              <p className="eyebrow">02 / Корпоративный сервис</p>
              <h1>
                Моя заявка<span className="purple-dot">.</span>
              </h1>

              {request ? (
                <div className="request-card">
                  <p className="eyebrow">Доступ: {request.resource}</p>
                  <h2>Заявка {request.id}</h2>

                  <span className={`request-badge ${request.status}`}>
                    {request.status === 'approved'
                      ? 'Согласована'
                      : 'Ожидает согласования'}
                  </span>

                  <p>Заявка связана с шагом «{requestStep?.title ?? request.stepId}» плана для позиции {plan.position} в команде «{plan.team}».</p>

                  {request.status === 'pending' && (
                    <button
                      className="action-button"
                      disabled={busy}
                      onClick={() =>
                        void post(
                          `/api/requests/${request.id}/approve`,
                          plan.mentor.id,
                        )
                      }
                    >
                      Демо: согласовать заявку ↗
                    </button>
                  )}
                </div>
              ) : (
                <div className="request-card">
                  <h2>Заявки пока нет</h2>
                  <p>{accessStep ? `Вернись к плану: заявка создаётся на шаге «${accessStep.title}».` : 'В этом плане пока нет шага для заявки.'}</p>
                  <button
                    className="action-button"
                    onClick={() => setPage('plan')}
                  >
                    К плану ↗
                  </button>
                </div>
              )}
            </section>
          )}

          {plan && page === 'absence' && (
            <section className="inner-page scenario-page" aria-labelledby="absence-title">
              <p className="eyebrow">Демо / Сервис отсутствий</p>
              <h1 id="absence-title">Отгул на день<span className="purple-dot">.</span></h1>
              <p className="scenario-intro">Синтетический сценарий: выбери один день, проверь дату и подтверди заявку. Это не реальный кадровый сервис.</p>
              <div className="scenario-layout">
                <form className="scenario-panel" onSubmit={(event) => {
                  event.preventDefault()
                  if (absenceDate >= new Date().toISOString().slice(0, 10)) setAbsenceConfirm(absenceDate)
                }}>
                  <h2>Выбрать дату</h2>
                  <label htmlFor="absence-date">День отгула</label>
                  <input id="absence-date" type="date" required min={new Date().toISOString().slice(0, 10)}
                    value={absenceDate} onChange={(event) => setAbsenceDate(event.target.value)} />
                  <p className="scenario-note">Только один день, сегодня или позже. Причину указывать не нужно.</p>
                  <button className="action-button" type="submit" disabled={absenceBusy || absenceLoading || !!absenceError || !absences || !absenceDate}>
                    Проверить заявку
                  </button>
                </form>
                <section className="scenario-panel scenario-history" aria-labelledby="absence-history-title">
                  <h2 id="absence-history-title">Мои заявки</h2>
                  {absenceLoading && <p role="status">Загружаем заявки на отгул…</p>}
                  {absenceError && !absenceLoading && (
                    <div className="scenario-error" role="alert">
                      <p>{absenceError}</p>
                      <button type="button" className="text-button" onClick={() => setAbsenceAttempt((attempt) => attempt + 1)} disabled={absenceBusy}>Повторить загрузку</button>
                    </div>
                  )}
                  {absenceReceipt && <p className="scenario-receipt" role="status">Заявка {absenceReceipt.id}: {absenceReceipt.status === 'approved' ? 'согласована' : 'ожидает согласования'}.</p>}
                  {!absenceLoading && !absenceError && absences?.length === 0 && <p>Заявок пока нет. Выбери день для первого отгула.</p>}
                  {!absenceLoading && !absenceError && !!absences?.length && (
                    <ul className="scenario-records">
                      {absences.map((item) => (
                        <li key={item.id}>
                          <strong>{item.date}</strong>
                          <span>Заявка {item.id}</span>
                          <span className={`request-badge ${item.status}`}>{item.status === 'approved' ? 'Согласована' : 'Ожидает согласования'}</span>
                          {item.status === 'pending' && (
                            <button type="button" className="text-button" disabled={absenceBusy}
                              onClick={() => void approveAbsence(item.id)}>Демо: согласовать как наставник</button>
                          )}
                        </li>
                      ))}
                    </ul>
                  )}
                </section>
              </div>
            </section>
          )}

          {plan && page === 'mentor' && (
            <section className="inner-page scenario-page" aria-labelledby="mentor-title">
              <p className="eyebrow">Демо / Корпоративный мессенджер</p>
              <h1 id="mentor-title">Написать наставнику<span className="purple-dot">.</span></h1>
              <p className="scenario-intro">Сообщение останется в синтетическом демо-мессенджере. В реальный чат оно не отправляется.</p>
              {plan.mentor?.id ? (
                <div className="scenario-layout">
                  <form className="scenario-panel" onSubmit={(event) => {
                    event.preventDefault()
                    const text = mentorDraft.trim()
                    if (text && Array.from(text).length <= 500) setMessageConfirm(text)
                  }}>
                    <p className="scenario-recipient">Наставник по назначенному плану <strong>{plan.mentor.name}</strong><span>{plan.mentor.channel}</span></p>
                    <label htmlFor="mentor-message">Сообщение наставнику</label>
                    <textarea id="mentor-message" rows={6} value={mentorDraft}
                      onChange={(event) => setMentorDraft(event.target.value)} />
                    <p className="scenario-note">Черновик можно изменить. До подтверждения ничего не отправляется. До 500 символов.</p>
                    <button className="action-button" type="submit" disabled={messageBusy || messagesLoading || !!messagesError || !messages || messagesKey !== mentorKey || !mentorDraft.trim() || Array.from(mentorDraft.trim()).length > 500}>
                      Проверить сообщение
                    </button>
                  </form>
                  <section className="scenario-panel scenario-history" aria-labelledby="message-history-title">
                    <h2 id="message-history-title">История сообщений</h2>
                    {messagesLoading && <p role="status">Загружаем историю сообщений…</p>}
                    {messagesError && !messagesLoading && (
                      <div className="scenario-error" role="alert">
                        <p>{messagesError}</p>
                        <button type="button" className="text-button" onClick={() => setMessagesAttempt((attempt) => attempt + 1)} disabled={messageBusy}>Повторить загрузку</button>
                      </div>
                    )}
                    {messageReceipt && <p className="scenario-receipt" role="status">Сообщение {messageReceipt.id}: отправлено в демо-мессенджер.</p>}
                    {!messagesLoading && !messagesError && messagesKey === mentorKey && messages?.length === 0 && <p>Сообщений пока нет. Можно начать с вопроса о первой задаче.</p>}
                    {!messagesLoading && !messagesError && messagesKey === mentorKey && !!messages?.length && (
                      <ul className="scenario-records">
                        {messages.map((item) => (
                          <li key={item.id}>
                            <span>Сообщение {item.id} · Отправлено</span>
                            <p>{item.text}</p>
                          </li>
                        ))}
                      </ul>
                    )}
                  </section>
                </div>
              ) : <p role="alert">В назначенном плане нет наставника. Сообщение отправить нельзя.</p>}
            </section>
          )}

          {plan && page === 'materials' && (
            <section className="inner-page materials-page" aria-labelledby="materials-title">
              <p className="eyebrow">Материалы / {plan.role}</p>
              <h1 id="materials-title">Материалы<span className="purple-dot">.</span></h1>
              <p className="materials-intro">Здесь отдельно собраны демо-инструкции для назначенного маршрута и публичные ссылки из кейса.</p>
              {materialsLoading && <p role="status">Загружаем материалы…</p>}
              {materialsError && !materialsLoading && (
                <div className="error-banner" role="alert">
                  <span>{materialsError}</span>
                  <button type="button" className="text-button" onClick={() => setMaterialsAttempt((attempt) => attempt + 1)}>Повторить ↗</button>
                </div>
              )}
              {!materialsLoading && !materialsError && materialsKey === planKey && materials && (() => {
                const instructions = materials.filter((item) => !item.url)
                const references = materials.filter((item) => item.url)
                const selected = instructions.find((item) => item.id === selectedMaterialId) ?? instructions[0]
                return (
                  <>
                    <section className="materials-section" aria-labelledby="instructions-heading">
                      <h2 id="instructions-heading">Инструкции демо-маршрута</h2>
                      <p>Материалы для выбранной роли и плана. Выбери документ, чтобы прочитать его целиком.</p>
                      {instructions.length ? (
                        <div className="materials-layout">
                          <nav className="materials-list" aria-label="Документы демо-маршрута">
                            {instructions.map((material) => (
                              <button key={material.id} type="button"
                                className={material.id === selected.id ? 'selected' : ''}
                                aria-current={material.id === selected.id ? 'true' : undefined}
                                onClick={() => setSelectedMaterialId(material.id)}>
                                {material.title}
                              </button>
                            ))}
                          </nav>
                          <article className="material-detail" aria-labelledby="material-title">
                            <h3 id="material-title">{selected.title}</h3>
                            <div className="material-content">{selected.content}</div>
                          </article>
                        </div>
                      ) : <p role="status">Для твоего маршрута пока нет инструкций.</p>}
                    </section>
                    {references.length > 0 && (
                      <section className="materials-section public-references" aria-labelledby="references-heading">
                        <h2 id="references-heading">Открытые источники из кейса</h2>
                        <p>Публичные ссылки для самостоятельного изучения. Они не являются проверенными внутренними инструкциями, не входят в материалы ответов AI-buddy и не заменяют назначенный план.</p>
                        <ul className="references-grid">
                          {references.map((material) => (
                            <li key={material.id}>
                              <a href={material.url} target="_blank" rel="noopener noreferrer"
                                aria-label={`${material.title} — внешний сайт, откроется в новой вкладке`}>
                                <span>{material.title}</span><span aria-hidden="true">↗</span>
                              </a>
                              <p>{material.content}</p>
                              <small>Внешний сайт · новая вкладка</small>
                            </li>
                          ))}
                        </ul>
                      </section>
                    )}
                  </>
                )
              })()}
            </section>
          )}

          {plan && page === 'buddy' && (
            <section className="inner-page buddy-page">
              <p className="eyebrow">03 / Помощник</p>
              <h1>Спроси AI-buddy<span className="purple-dot">.</span></h1>
              <p className="buddy-context">Твой маршрут: {plan.role}, команда «{plan.team}». Ответы основаны на доступных помощнику материалах.</p>

              <div className="buddy-conversation" role="log" aria-label="Разговор с AI-buddy" aria-live="polite" aria-relevant="additions text">
                <article className="buddy-message buddy-intro">
                  <span className="buddy-speaker">Начало разговора</span>
                  <p>Привет! Спроси о первом дне, команде или следующем шаге. Я подскажу, где искать ответ.</p>
                </article>
                {turns.map((turn, index) => (
                  <div className="buddy-turn" key={index}>
                    <article className="buddy-message buddy-user">
                      <span className="buddy-speaker">Ты</span>
                      <p>{turn.question}</p>
                    </article>
                    {turn.answer !== undefined && (
                      <article className="buddy-message buddy-answer">
                        <span className="buddy-speaker">{turn.toolCall === 'get_request_status' ? 'MCP · статус заявки' : turn.model ? 'AI-buddy' : 'Системная подсказка'}</span>
                        {renderAnswer(turn)}
                        {turn.suggestedAction === 'message_mentor' && (
                          <button type="button" className="action-button buddy-mentor-action" onClick={() => setPage('mentor')}>
                            Написать наставнику ↗
                          </button>
                        )}
                      </article>
                    )}
                    {turn.error && (
                      <div className="buddy-error" role="alert">
                        <p>{turn.error}</p>
                        <button type="button" className="text-button" disabled={answering} onClick={() => void askBuddy(turn.question, index)}>Повторить вопрос ↗</button>
                      </div>
                    )}
                  </div>
                ))}
                {answering && <p className="buddy-loading" role="status">AI-buddy готовит ответ…</p>}
              </div>

              <form className="buddy-form" onSubmit={(event) => {
                event.preventDefault()
                void askBuddy(question)
              }}>
                <label htmlFor="buddy-question">Твой вопрос</label>
                <div className="buddy-compose">
                  <textarea id="buddy-question" value={question} onChange={(event) => setQuestion(event.target.value)}
                    onKeyDown={(event) => {
                      if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
                        event.preventDefault()
                        event.currentTarget.form?.requestSubmit()
                      }
                    }}
                    placeholder="Например: как получить доступ к рабочим инструментам?"
                    rows={2} maxLength={500} disabled={answering} />
                  <button className="action-button" type="submit" disabled={answering || !question.trim()}>Отправить ↗</button>
                </div>
                <p className="buddy-hint">Enter — отправить, Shift + Enter — новая строка. Не вводи реальные персональные данные.</p>
              </form>
            </section>
          )}
        </main>
      </div>

      {absenceConfirm && (
        <ConfirmDialog title="Создать заявку на отгул?" confirmLabel="Создать заявку" busy={absenceBusy}
          onCancel={() => setAbsenceConfirm(null)}
          onConfirm={() => {
            const date = absenceConfirm
            setAbsenceConfirm(null)
            void createAbsence(date)
          }}>
          <p>Один день: <strong>{absenceConfirm}</strong>. Заявка появится в демо-сервисе со статусом ожидания согласования.</p>
        </ConfirmDialog>
      )}
      {messageConfirm && plan?.mentor?.id && (
        <ConfirmDialog title="Отправить сообщение?" confirmLabel="Отправить наставнику" busy={messageBusy}
          onCancel={() => setMessageConfirm(null)}
          onConfirm={() => {
            const text = messageConfirm
            setMessageConfirm(null)
            void sendMentorMessage(text, plan.mentor)
          }}>
          <p>Получатель: <strong>{plan.mentor.name}</strong> ({plan.mentor.channel}). Текст сообщения:</p>
          <blockquote className="scenario-preview">{messageConfirm}</blockquote>
          <p>Отправка только в синтетический демо-мессенджер.</p>
        </ConfirmDialog>
      )}

      {confirmStep?.resource && plan && (
        <div className="modal-backdrop">
          <div
            className="confirm-modal"
            role="dialog"
            aria-modal="true"
            aria-labelledby="confirm-title"
          >
            <p className="eyebrow">Подтверждение / {confirmStep.title}</p>
            <h2 id="confirm-title">Запросить доступ?</h2>
            <p>
              Создадим заявку на доступ к «{confirmStep.resource}» для команды
              «{plan.team}». После отправки её должен согласовать наставник.
            </p>

            <div className="confirm-actions">
              <button
                className="cancel-button"
                onClick={() => setConfirmStepId(null)}
              >
                Отмена
              </button>
              <button
                className="action-button"
                disabled={busy}
                onClick={() => {
                  setConfirmStepId(null)
                  void post('/api/requests', 'intern-1')
                }}
              >
                Создать заявку ↗
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

export default App
