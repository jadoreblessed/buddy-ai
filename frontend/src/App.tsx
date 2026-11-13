import { useCallback, useEffect, useState } from 'react'
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

type Plan = {
  planId: string
  intern: string
  position: 'backend' | 'frontend' | 'analyst'
  role: string
  team: string
  progress: number
  steps: Step[]
  request: AccessRequest | null
}
type Page = 'plan' | 'buddy' | 'request'

const API = import.meta.env.VITE_API_URL || 'http://localhost:8080'

const statusText: Record<Status, string> = {
  done: 'Выполнено',
  current: 'Сейчас',
  pending: 'Ожидает согласования',
  locked: 'Следующий шаг',
}

function App() {
  const [plan, setPlan] = useState<Plan | null>(null)
  const [page, setPage] = useState<Page>('plan')
  const [busy, setBusy] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [confirmStepId, setConfirmStepId] = useState<string | null>(null)

  const loadPlan = useCallback(async () => {
    setLoading(true)
    try {
      const response = await fetch(`${API}/api/plan`)
      if (!response.ok) {
        const body = await response.text()
        let message = body.trim()
        try {
          const data = JSON.parse(body) as { error?: string }
          message = data.error || message
        } catch {
          // Non-JSON API errors are already readable as text.
        }
        throw new Error(message || `Не удалось загрузить план (${response.status})`)
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
          </nav>

          <div className="sidebar-note">
            <span>✳</span>
            <p>Не нужно разбираться во всём сразу. Двигайся по одному шагу.</p>
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
                  <p className="eyebrow">День 01 / Адаптация</p>
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
                          'mentor-1',
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

          {plan && page === 'buddy' && (
            <section className="inner-page">
              <p className="eyebrow">03 / Помощник</p>
              <h1>
                Спроси AI-buddy<span className="purple-dot">.</span>
              </h1>

              <div className="request-card">
                <span className="helper-icon">✳</span>
                <h2>Что делать сейчас?</h2>
                <p>
                  {request?.status === 'pending'
                    ? `Заявка ${request.id} на «${request.resource}» ожидает согласования. Её статус можно посмотреть в разделе «Моя заявка».`
                    : currentStep
                      ? `Сейчас: ${currentStep.title}. ${currentStep.detail}`
                      : 'Все шаги плана выполнены.'}
                </p>

                <button
                  className="action-button"
                  onClick={() => setPage('plan')}
                >
                  Открыть план ↗
                </button>
              </div>

              <p className="page-footnote">
                Подсказка составлена по текущему шагу назначенного плана.
              </p>
            </section>
          )}
        </main>
      </div>

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