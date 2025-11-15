interface NewsItemProps {
  item: {
    id: number
    title: string
    url: string
    source: string
    points: number
    comments: number
    timeAgo: string
    author: string
  }
  index: number
}

export function NewsItem({ item, index }: NewsItemProps) {
  const domain = new URL(item.url).hostname.replace('www.', '')
  
  return (
    <article className="flex gap-3 py-3 px-2 hover:bg-muted/50 rounded-lg transition-colors">
      {/* Index number */}
      <div className="text-muted-foreground text-sm font-mono w-6 flex-shrink-0 pt-1">
        {index}.
      </div>

      {/* Content */}
      <div className="flex-1 min-w-0">
        {/* Title and domain */}
        <div className="flex flex-wrap items-baseline gap-2 mb-1">
          <a
            href={item.url}
            target="_blank"
            rel="noopener noreferrer"
            className="text-foreground hover:text-[#ff6b35] transition-colors font-medium text-base leading-snug"
          >
            {item.title}
          </a>
          <span className="text-muted-foreground text-xs">
            ({domain})
          </span>
        </div>

        {/* Metadata */}
        <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
          <span className="flex items-center gap-1">
            <span className="text-sm">🐾</span>
            {item.points} points
          </span>
          <span>by {item.author}</span>
          <span>{item.timeAgo}</span>
          <span className="flex items-center gap-1">
            <span className="text-sm">💬</span>
            {item.comments} comments
          </span>
          <span className="text-[#ff6b35] font-medium">
            {item.source}
          </span>
        </div>
      </div>
    </article>
  )
}
