'use client'

import { useState } from 'react'

interface Source {
  id: number
  name: string
  url: string
  category: string
  isActive: boolean
}

const mockSources: Source[] = [
  { id: 1, name: 'Tech Meows', url: 'https://techmeows.com/feed', category: 'tech', isActive: true },
  { id: 2, name: 'Dev Digest', url: 'https://devdigest.io/rss', category: 'tech', isActive: true },
  { id: 3, name: 'Startup News', url: 'https://startupnews.co/feed.xml', category: 'business', isActive: true },
  { id: 4, name: 'Programming Weekly', url: 'https://progweekly.com/api/feed', category: 'tech', isActive: false },
]

export function SourceList() {
  const [sources, setSources] = useState(mockSources)

  const handleDelete = (id: number) => {
    if (confirm('Are you sure you want to remove this source? 😿')) {
      setSources(sources.filter(s => s.id !== id))
    }
  }

  const handleToggle = (id: number) => {
    setSources(sources.map(s => 
      s.id === id ? { ...s, isActive: !s.isActive } : s
    ))
  }

  const getCategoryEmoji = (category: string) => {
    const map: Record<string, string> = {
      tech: '💻',
      news: '📰',
      science: '🔬',
      business: '💼',
      other: '📌'
    }
    return map[category] || '📌'
  }

  return (
    <div className="space-y-3">
      {sources.map((source) => (
        <div
          key={source.id}
          className={`flex items-center justify-between p-4 rounded-lg border transition-all ${
            source.isActive 
              ? 'bg-background border-border' 
              : 'bg-muted/30 border-border opacity-60'
          }`}
        >
          <div className="flex items-center gap-3 flex-1 min-w-0">
            <span className="text-2xl">{getCategoryEmoji(source.category)}</span>
            <div className="flex-1 min-w-0">
              <h3 className="font-medium text-foreground truncate">
                {source.name}
              </h3>
              <p className="text-xs text-muted-foreground truncate">
                {source.url}
              </p>
            </div>
            <span className={`text-xs px-2 py-1 rounded-full ${
              source.isActive 
                ? 'bg-[#ff6b35]/10 text-[#ff6b35]' 
                : 'bg-muted text-muted-foreground'
            }`}>
              {source.isActive ? 'Active' : 'Inactive'}
            </span>
          </div>

          <div className="flex items-center gap-2 ml-4">
            <button
              onClick={() => handleToggle(source.id)}
              className="p-2 hover:bg-muted rounded-md transition-colors text-lg"
              title={source.isActive ? 'Deactivate' : 'Activate'}
            >
              {source.isActive ? '⏸️' : '▶️'}
            </button>
            <button
              onClick={() => handleDelete(source.id)}
              className="p-2 hover:bg-destructive/10 hover:text-destructive rounded-md transition-colors text-lg"
              title="Delete source"
            >
              🗑️
            </button>
          </div>
        </div>
      ))}

      {sources.length === 0 && (
        <div className="text-center py-12 text-muted-foreground">
          <div className="text-4xl mb-3">😿</div>
          <p>No sources configured yet.</p>
          <p className="text-sm mt-1">Add your first source above to get started!</p>
        </div>
      )}
    </div>
  )
}
