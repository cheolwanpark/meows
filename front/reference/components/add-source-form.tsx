'use client'

import { useState } from 'react'

export function AddSourceForm() {
  const [name, setName] = useState('')
  const [url, setUrl] = useState('')
  const [category, setCategory] = useState('tech')

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    // In production, this would save to a database
    console.log('Adding source:', { name, url, category })
    // Reset form
    setName('')
    setUrl('')
    setCategory('tech')
    alert(`Source "${name}" added! 🐱`)
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <div>
        <label htmlFor="name" className="block text-sm font-medium text-foreground mb-1.5">
          Source Name
        </label>
        <input
          type="text"
          id="name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="e.g., Tech Meows"
          required
          className="w-full px-3 py-2 bg-background border border-input rounded-md text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-[#ff6b35] focus:border-transparent"
        />
      </div>

      <div>
        <label htmlFor="url" className="block text-sm font-medium text-foreground mb-1.5">
          RSS Feed or API URL
        </label>
        <input
          type="url"
          id="url"
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          placeholder="https://example.com/feed.xml"
          required
          className="w-full px-3 py-2 bg-background border border-input rounded-md text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-[#ff6b35] focus:border-transparent"
        />
      </div>

      <div>
        <label htmlFor="category" className="block text-sm font-medium text-foreground mb-1.5">
          Category
        </label>
        <select
          id="category"
          value={category}
          onChange={(e) => setCategory(e.target.value)}
          className="w-full px-3 py-2 bg-background border border-input rounded-md text-foreground focus:outline-none focus:ring-2 focus:ring-[#ff6b35] focus:border-transparent"
        >
          <option value="tech">Technology</option>
          <option value="news">General News</option>
          <option value="science">Science</option>
          <option value="business">Business</option>
          <option value="other">Other</option>
        </select>
      </div>

      <button
        type="submit"
        className="w-full sm:w-auto px-6 py-2.5 bg-[#ff6b35] text-white font-medium rounded-md hover:bg-[#e55a2b] transition-colors focus:outline-none focus:ring-2 focus:ring-[#ff6b35] focus:ring-offset-2"
      >
        Add Source 🐾
      </button>
    </form>
  )
}
