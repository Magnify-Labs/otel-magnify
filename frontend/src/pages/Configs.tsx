import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { configsAPI } from '../api/client'
import YamlEditor from '../components/config/YamlEditor'

export default function Configs() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { data: configs, isLoading } = useQuery({ queryKey: ['configs'], queryFn: configsAPI.list })

  const [name, setName] = useState('')
  const [content, setContent] = useState('')
  const [showForm, setShowForm] = useState(false)

  const createMutation = useMutation({
    mutationFn: () => configsAPI.create(name, content),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['configs'] })
      setName('')
      setContent('')
      setShowForm(false)
    },
  })

  return (
    <div>
      <div className="page-header">
        <h1 className="page-title">{t('configs.title')}</h1>
        <button
          className={`btn ${showForm ? '' : 'btn-primary'}`}
          onClick={() => setShowForm(!showForm)}
        >
          {showForm ? t('common.cancel') : t('configs.new_button')}
        </button>
      </div>

      {showForm && (
        <div className="configs-form">
          <div className="configs-form-header">{t('configs.form.header')}</div>
          <div className="configs-form-body">
            <div className="field">
              <label className="field-label">{t('configs.form.name')}</label>
              <input
                className="field-input"
                placeholder={t('configs.form.name_placeholder')}
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </div>
            <div className="field" style={{ marginBottom: 0 }}>
              <label className="field-label">{t('configs.form.content')}</label>
              <YamlEditor value={content} onChange={setContent} />
            </div>
          </div>
          <div className="configs-form-footer">
            <button
              className="btn btn-primary"
              onClick={() => createMutation.mutate()}
              disabled={!name || !content || createMutation.isPending}
            >
              {createMutation.isPending ? t('configs.form.saving') : t('configs.form.submit')}
            </button>
          </div>
        </div>
      )}

      {isLoading ? (
        <div className="loading">{t('configs.loading')}</div>
      ) : (configs ?? []).length === 0 ? (
        <div className="empty-state">{t('configs.empty')}</div>
      ) : (
        <table className="data-table">
          <thead>
            <tr>
              <th>{t('configs.table.name')}</th>
              <th>{t('configs.table.created_by')}</th>
              <th>{t('configs.table.created_at')}</th>
              <th>{t('configs.table.id')}</th>
            </tr>
          </thead>
          <tbody>
            {(configs ?? []).map((c) => (
              <tr key={c.id}>
                <td style={{ fontFamily: 'var(--mono)', color: 'var(--text-hi)' }}>{c.name}</td>
                <td style={{ fontFamily: 'var(--mono)', fontSize: '0.8rem' }}>{c.created_by}</td>
                <td
                  style={{
                    fontFamily: 'var(--mono)',
                    fontSize: '0.75rem',
                    color: 'var(--muted)',
                    whiteSpace: 'nowrap',
                  }}
                >
                  {new Date(c.created_at).toLocaleString()}
                </td>
                <td>
                  <code>{c.id.substring(0, 12)}...</code>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
