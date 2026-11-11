import { useCallback, useEffect, useState } from 'react'
import './App.css'

type Status = 'done' | 'current' | 'pending' | 'locked'

type Step = {
  id: number
  title: string
  detail: string
  status: Status
}

type AccessRequest = {
  id: string
  resource: string
  status: string
  createdAt: string
  approvedAt?: string
}

type Plan = {
  intern: string
  role: string
  team: string
  progress: number
  steps: Step[]
  request: AccessRequest | null
}

type Page = 'plan' | 'buddy' | 'request'

const API = 'http://localhost:8080'

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
  const [error, setError] = useState('')

  const loadPlan = useCallback(async () => {
    try {
      const response = await fetch(`${API}/api/plan`)
      if (!response.ok) throw new Error('Не удалось загрузить план')
      setPlan((await response.json()) as Plan)
      setError('')
    } catch {
      setError('Нет связи с Go-сервером. Запусти go run ./backend.')
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

  const request = plan?.request
  const total = plan?.steps.length ?? 4

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <button className="brand" onClick={() => setPage('plan')} aria-label="На главную">
          <span className="brand-mark">
            <i /><i /><i /><i />
          </span>
          <span>рядом<span className="brand-dot">.</span></span>
        </button>

        <div className="sidebar-middle">
          <p className="sidebar-caption">Твой первый день</p>
          <nav className="navigation" aria-label="Разделы">
            <button className={page === 'plan' ? 'active' : ''} onClick={() => setPage('plan')}>
              <span>01</span> План адаптации
            </button>
            <button className={page === 'buddy' ? 'active' : ''} onClick={() => setPage('buddy')}>
              <span className="nav-star">✳</span> Спросить AI-buddy
            </button>
            <button className={page === 'request' ? 'active' : ''} onClick={() => setPage('request')}>
              <span>↗</span> Моя заявка
            </button>
          </nav>

          <div className="sidebar-note">
            <span>✳</span>
            <p>Не нужно разбираться во всём сразу. Двигайся по одному шагу.</p>
          </div>
        </div>

        <div className="profile">
          <span className="avatar">М</span>
          <span>
            <strong>{plan?.intern ?? 'Маша Ковалёва'}</strong>
            <small>{plan ? `${plan.role} · ${plan.team}` : 'Стажёр'}</small>
          </span>
        </div>
      </aside>

      <div className="workspace">
        <header className="topbar">
          <span>Стажировка / Первый день</span>
          <span>Демо-прототип <b>•</b></span>
        </header>

        <main className="content">
          {error && <div className="error-banner">{error}</div>}

          {page === 'plan' && (
            <>
              <section className="hero">
                <div className="hero-copy">
                  <p className="eyebrow">День 01 / Адаптация</p>
                  <h1><mark>Первый день.</mark><br />Давай по шагам.</h1>
                  <p className="hero-description">
                    Знакомство, инструменты, доступы и первая задача — всё в одном маршруте.
                    Неясно, что делать дальше? Открой подсказки помощника.
                  </p>
                </div>
                <div className="sticky-note" aria-hidden="true">
                  <span>твоя точка<br />старта</span>
                  <b>01—04</b>
                  <i>✳</i>
                </div>
              </section>

              <section className="plan-section">
                <div className="section-heading">
                  <div>
                    <p className="eyebrow">01 / Твой маршрут</p>
                    <h2>План первого дня</h2>
                    <p className="section-subtitle">
                      {request?.status === 'approved'
                        ? 'Доступ согласован. Можно переходить к первой задаче.'
                        : request
                          ? 'Заявка на доступ ожидает согласования.'
                          : 'Следующий шаг — получить доступ к тестовому окружению.'}
                    </p>
                  </div>
                  <strong className="progress-label">{plan?.progress ?? 0} из {total}</strong>
                </div>

                <div
                  className="progress-track"
                  role="progressbar"
                  aria-valuenow={plan?.progress ?? 0}
                  aria-valuemin={0}
                  aria-valuemax={total}
                >
                  <span style={{ width: `${((plan?.progress ?? 0) / total) * 100}%` }} />
                </div>

                <div className="steps">
                  {plan?.steps.map((step) => (
                    <article className={`step step-${step.status}`} key={step.id}>
                      <span className="step-number">{String(step.id).padStart(2, '0')}</span>
                      <div className="step-body">
                        <h3>{step.title}</h3>
                        <p>{step.detail}</p>
                        {step.id === 3 && step.status === 'current' && (
                          <button
                            className="action-button"
                            disabled={busy}
                            onClick={() => void post('/api/requests', 'intern-1')}
                          >
                            {busy ? 'Создаём...' : 'Создать заявку ↗'}
                          </button>
                        )}
                        {step.id === 3 && step.status === 'pending' && (
                          <button className="text-button" onClick={() => setPage('request')}>
                            Заявка {request?.id} · посмотреть статус ↗
                          </button>
                        )}
                      </div>
                      <span className="step-status">{statusText[step.status]}</span>
                    </article>
                  ))}
                </div>
              </section>
            </>
          )}

          {page === 'request' && (
            <section className="inner-page">
              <p className="eyebrow">02 / Корпоративный сервис</p>
              <h1>Моя заявка<span className="purple-dot">.</span></h1>
              {request ? (
                <div className="request-card">
                  <p className="eyebrow">Доступ к тестовому окружению</p>
                  <h2>Заявка {request.id}</h2>
                  <span className={`request-badge ${request.status}`}>
                    {request.status === 'approved' ? 'Согласована' : 'Ожидает согласования'}
                  </span>
                  <p>Заявка сохранена и связана с третьим шагом плана адаптации.</p>
                  {request.status === 'pending' && (
                    <button
                      className="action-button"
                      disabled={busy}
                      onClick={() => void post(`/api/requests/${request.id}/approve`, 'mentor-1')}
                    >
                      Демо: согласовать заявку ↗
                    </button>
                  )}
                </div>
              ) : (
                <div className="request-card">
                  <h2>Заявки пока нет</h2>
                  <p>Вернись к плану и создай её на третьем шаге.</p>
                  <button className="action-button" onClick={() => setPage('plan')}>
                    К плану ↗
                  </button>
                </div>
              )}
            </section>
          )}

          {page === 'buddy' && (
            <section className="inner-page">
              <p className="eyebrow">03 / Помощник</p>
              <h1>Спроси AI-buddy<span className="purple-dot">.</span></h1>
              <div className="request-card">
                <span className="helper-icon">✳</span>
                <h2>Что делать сейчас?</h2>
                <p>
                  {request?.status === 'approved'
                    ? 'Доступ согласован. Следующий шаг — приступить к первой задаче вместе с наставником.'
                    : request
                      ? `Заявка ${request.id} ожидает согласования. Её статус можно посмотреть в разделе «Моя заявка».`
                      : 'Для первой задачи нужен доступ к тестовому окружению. Создай заявку в плане адаптации.'}
                </p>
                <button className="action-button" onClick={() => setPage('plan')}>
                  Открыть план ↗
                </button>
              </div>
              <p className="page-footnote">Сейчас здесь подсказка по данным плана. Диалог с моделью подключим отдельно.</p>
            </section>
          )}
        </main>
      </div>
    </div>
  )
}

export default App