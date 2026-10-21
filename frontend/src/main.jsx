import React, { useEffect, useMemo, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { Activity, CalendarDays, ChevronDown, CirclePlus, Pencil, RefreshCw, Search, Trash2, Wallet } from 'lucide-react'
import './styles.css'

const categories = ['еда', 'транспорт', 'жильё', 'развлечения', 'здоровье', 'покупки', 'другое']
const today = () => { const date = new Date(); return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}` }
const blankForm = () => ({ user_id: '', amount: '', category: 'еда', spent_at: today(), comment: '' })
const money = value => new Intl.NumberFormat('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(Number(value || 0))
const dateLabel = value => value ? new Date(`${String(value).slice(0, 10)}T12:00:00`).toLocaleDateString('ru-RU') : '—'

async function api(path, options = {}) {
  const response = await fetch(path, { ...options, headers: { 'Content-Type': 'application/json', ...(options.headers || {}) } })
  if (!response.ok) {
    const data = await response.json().catch(() => ({}))
    throw new Error(data.error || `Ошибка запроса (${response.status})`)
  }
  if (response.status === 204) return null
  return response.json()
}

function App() {
  const [expenses, setExpenses] = useState([])
  const [members, setMembers] = useState([])
  const [filters, setFilters] = useState({ user_id: '', from: '', to: '', category: '' })
  const [form, setForm] = useState(blankForm())
  const [editingId, setEditingId] = useState(null)
  const [showForm, setShowForm] = useState(false)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')

  const query = useMemo(() => {
    const params = new URLSearchParams()
    if (filters.user_id) params.set('user_id', filters.user_id)
    if (filters.from) params.set('from', filters.from)
    if (filters.to) params.set('to', filters.to)
    if (filters.category) params.set('category', filters.category)
    return params.toString()
  }, [filters])

  async function loadMembers() {
    const data = await api('/api/users')
    setMembers(data)
  }

  async function loadExpenses() {
    setLoading(true)
    try {
      const data = await api(`/api/expenses${query ? `?${query}` : ''}`)
      setExpenses(data)
      setError('')
    } catch (e) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadMembers().catch(e => setError(e.message))
  }, [])

  useEffect(() => {
    loadExpenses()
  }, [query])

  const total = expenses.reduce((sum, item) => sum + Number(item.amount), 0)
  const average = expenses.length ? total / expenses.length : 0
  const setFilter = (key, value) => setFilters(current => ({ ...current, [key]: value }))
  const setField = (key, value) => setForm(current => ({ ...current, [key]: value }))

  function startCreate() {
    setForm({ ...blankForm(), user_id: filters.user_id || (members[0] ? String(members[0].telegram_id) : '') })
    setEditingId(null)
    setShowForm(true)
    setError('')
    setNotice('')
  }

  function startEdit(expense) {
    setForm({ user_id: String(expense.user_id), amount: String(expense.amount), category: expense.category, spent_at: String(expense.spent_at).slice(0, 10), comment: expense.comment || '' })
    setEditingId(expense.id)
    setShowForm(true)
    setError('')
    setNotice('')
    window.scrollTo({ top: 0, behavior: 'smooth' })
  }

  async function submitForm(event) {
    event.preventDefault()
    setSaving(true)
    setError('')
    setNotice('')
    const payload = { ...form, user_id: Number(form.user_id), amount: Number(form.amount) }
    try {
      await api(editingId ? `/api/expenses/${editingId}` : '/api/expenses', { method: editingId ? 'PUT' : 'POST', body: JSON.stringify(payload) })
      setShowForm(false)
      setNotice(editingId ? 'Изменения сохранены.' : 'Трата добавлена.')
      await loadExpenses()
    } catch (e) {
      setError(e.message)
    } finally {
      setSaving(false)
    }
  }

  async function removeExpense(expense) {
    const label = expense.comment || expense.category
    if (!window.confirm(`Удалить трату «${label}» на ${money(expense.amount)}?`)) return
    try {
      await api(`/api/expenses/${expense.id}`, { method: 'DELETE' })
      setNotice('Трата удалена.')
      setError('')
      await loadExpenses()
    } catch (e) {
      setError(e.message)
    }
  }

  const memberName = id => members.find(member => Number(member.telegram_id) === Number(id))?.display_name || `ID ${id}`

  return <div className="app-shell">
    <header className="topbar">
      <div className="brand"><div className="brand-mark"><Wallet size={22} /></div><div><div className="brand-name">Общий бюджет</div><div className="brand-caption">Семейные расходы</div></div></div>
      <div className="topbar-right"><span className="status-dot"></span><span>Панель учёта</span><button className="icon-button" title="Обновить данные" onClick={() => { loadMembers().catch(e => setError(e.message)); loadExpenses() }}><RefreshCw size={17} /></button></div>
    </header>

    <main className="main-content">
      <section className="page-heading"><div><p className="eyebrow">ФИНАНСЫ СЕМЬИ</p><h1>Расходы</h1><p className="subtitle">Учитывайте покупки, сравнивайте категории и следите за общими тратами.</p></div><button className="primary-button" onClick={startCreate}><CirclePlus size={18} /> Добавить трату</button></section>

      {error && <div className="alert error-alert">{error}</div>}
      {notice && <div className="alert success-alert">{notice}</div>}

      <section className="stats-grid">
        <article className="stat-card"><div className="stat-top"><span>Всего расходов в выборке</span><span className="stat-icon blue"><Wallet size={18} /></span></div><div className="stat-value">{money(total)}</div><div className="stat-foot">Сумма по текущим фильтрам</div></article>
        <article className="stat-card"><div className="stat-top"><span>Количество операций</span><span className="stat-icon violet"><Activity size={18} /></span></div><div className="stat-value">{expenses.length}</div><div className="stat-foot">Отображаемые записи</div></article>
        <article className="stat-card"><div className="stat-top"><span>Средняя трата</span><span className="stat-icon amber"><CalendarDays size={18} /></span></div><div className="stat-value">{money(average)}</div><div className="stat-foot">На одну операцию</div></article>
      </section>

      {showForm && <section className="form-card"><div className="section-heading"><div><h2>{editingId ? 'Редактировать трату' : 'Новая трата'}</h2><p>Заполните данные операции.</p></div><button className="text-button" type="button" onClick={() => setShowForm(false)}>Закрыть</button></div>
        <form className="expense-form" onSubmit={submitForm}>
          <label>Член семьи<select value={form.user_id} onChange={e => setField('user_id', e.target.value)} required><option value="">Выберите пользователя</option>{members.map(m => <option key={m.telegram_id} value={m.telegram_id}>{m.display_name} · {m.telegram_id}</option>)}</select></label>
          <label>Сумма<input type="number" min="0.01" step="0.01" placeholder="350" value={form.amount} onChange={e => setField('amount', e.target.value)} required /></label>
          <label>Категория<select value={form.category} onChange={e => setField('category', e.target.value)}>{categories.map(c => <option key={c} value={c}>{c}</option>)}</select></label>
          <label>Дата<input type="date" value={form.spent_at} onChange={e => setField('spent_at', e.target.value)} required /></label>
          <label className="wide-field">Комментарий<input type="text" maxLength="500" placeholder="Кофе, такси, продукты…" value={form.comment} onChange={e => setField('comment', e.target.value)} /></label>
          <div className="form-actions"><button type="button" className="secondary-button" onClick={() => setShowForm(false)}>Отмена</button><button type="submit" className="primary-button" disabled={saving || !members.length}>{saving ? 'Сохранение…' : 'Сохранить'}</button></div>
          {!members.length && <p className="form-hint">Сначала каждый член семьи должен открыть бота в Telegram и отправить ему сообщение.</p>}
        </form>
      </section>}

      <section className="table-card"><div className="table-heading"><div><h2>История операций</h2><p>Фильтры применяются сразу.</p></div><div className="record-count">{expenses.length} записей</div></div>
        <div className="filters">
          <label className="search-filter"><Search size={16} /><select aria-label="Член семьи" value={filters.user_id} onChange={e => setFilter('user_id', e.target.value)}><option value="">Все члены семьи</option>{members.map(m => <option key={m.telegram_id} value={m.telegram_id}>{m.display_name}</option>)}</select><ChevronDown size={14} /></label>
          <label className="date-filter"><span>С</span><input aria-label="Дата с" type="date" value={filters.from} onChange={e => setFilter('from', e.target.value)} /></label>
          <label className="date-filter"><span>По</span><input aria-label="Дата по" type="date" value={filters.to} onChange={e => setFilter('to', e.target.value)} /></label>
          <label className="category-filter"><select aria-label="Категория" value={filters.category} onChange={e => setFilter('category', e.target.value)}><option value="">Все категории</option>{categories.map(c => <option key={c} value={c}>{c}</option>)}</select><ChevronDown size={14} /></label>
          <button className="reset-button" onClick={() => setFilters({ user_id: '', from: '', to: '', category: '' })}>Сбросить</button>
        </div>
        <div className="table-wrap"><table><thead><tr><th>Описание</th><th>Член семьи</th><th>Категория</th><th>Дата</th><th className="amount-col">Сумма</th><th className="actions-col">Действия</th></tr></thead><tbody>
          {loading ? <tr><td colSpan="6" className="empty-state">Загрузка операций…</td></tr> : expenses.length === 0 ? <tr><td colSpan="6" className="empty-state"><div className="empty-icon"><Search size={22} /></div><strong>Траты не найдены</strong><span>Измените фильтры или добавьте первую запись.</span></td></tr> : expenses.map(e => <tr key={e.id}><td><div className="expense-title">{e.comment || e.category}</div><div className="expense-meta">Операция #{e.id}</div></td><td><div className="member-cell"><span className="avatar">{memberName(e.user_id).slice(0, 1).toUpperCase()}</span><span>{memberName(e.user_id)}</span></div></td><td><span className={`category-pill category-${categories.indexOf(e.category)}`}>{e.category}</span></td><td className="date-cell">{dateLabel(e.spent_at)}</td><td className="amount-col amount-value">{money(e.amount)}</td><td className="actions-col"><div className="row-actions"><button className="icon-button edit-button" title="Редактировать" onClick={() => startEdit(e)}><Pencil size={16} /></button><button className="icon-button delete-button" title="Удалить" onClick={() => removeExpense(e)}><Trash2 size={16} /></button></div></td></tr>)}
        </tbody></table></div>
        <div className="table-footer"><span>Показаны операции, соответствующие фильтрам</span><span>Максимум 1 000 записей за запрос</span></div>
      </section>
      <footer className="footer">Общий бюджет <span>·</span> Данные хранятся в вашей базе PostgreSQL</footer>
    </main>
  </div>
}

createRoot(document.getElementById('root')).render(<App />)
