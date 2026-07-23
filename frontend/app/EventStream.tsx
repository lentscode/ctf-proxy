import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getEvents, isUnauthorized, type ObserveEvent } from '../lib/api'
import { subscribeToEventStream, type StreamStatus } from '../lib/event-stream'

interface EventStreamProps {
  onUnauthorized: () => void
}

const maximumEvents = 100

function mergeEvents(...collections: ObserveEvent[][]): ObserveEvent[] {
  const unique = new Map<number, ObserveEvent>()
  for (const collection of collections) {
    for (const event of collection) {
      unique.set(event.id, event)
    }
  }
  return [...unique.values()].sort((left, right) => right.id - left.id).slice(0, maximumEvents)
}

export function EventStream({ onUnauthorized }: EventStreamProps) {
  const { data: history, error, isError, isLoading, refetch } = useQuery({ queryKey: ['events'], queryFn: getEvents })
  const [liveEvents, setLiveEvents] = useState<ObserveEvent[]>([])
  const [status, setStatus] = useState<StreamStatus>('connecting')
  const [hasNewEvents, setHasNewEvents] = useState(false)
  const scrollRef = useRef<HTMLDivElement>(null)
  const events = useMemo(() => mergeEvents(liveEvents, history ?? []), [history, liveEvents])

  useEffect(() => {
    if (isUnauthorized(error)) {
      onUnauthorized()
    }
  }, [error, onUnauthorized])

  useEffect(() => {
    const unsubscribe = subscribeToEventStream({
      onEvent(event) {
        const isReadingHistory = (scrollRef.current?.scrollTop ?? 0) > 12
        setLiveEvents((current) => mergeEvents([event], current))
        if (isReadingHistory) {
          setHasNewEvents(true)
        }
      },
      onConnected() {
        void refetch()
      },
      onStatus: setStatus,
      onUnauthorized,
    })
    return unsubscribe
  }, [onUnauthorized, refetch])

  function returnToNewest() {
    scrollRef.current?.scrollTo({ top: 0, behavior: 'smooth' })
    setHasNewEvents(false)
  }

  return (
    <section className="dashboard-panel events-panel" aria-labelledby="events-heading">
      <header className="panel-header">
        <h1 id="events-heading">Events</h1>
        <span className={`stream-status stream-${status}`}>{status}</span>
      </header>
      <div className="event-scroll" ref={scrollRef} onScroll={() => {
        if ((scrollRef.current?.scrollTop ?? 0) <= 12) setHasNewEvents(false)
      }}>
        {isLoading && events.length === 0 && <EventSkeleton />}
        {isError && events.length === 0 && !isUnauthorized(error) && (
          <div className="panel-message">
            <p>Unable to load events.</p>
            <button type="button" className="text-button" onClick={() => void refetch()}>Retry</button>
          </div>
        )}
        {!isLoading && !isError && events.length === 0 && <div className="panel-message">No events recorded.</div>}
        <ol className="event-list">
          {events.map((event) => <EventRow key={event.id} event={event} />)}
        </ol>
      </div>
      {hasNewEvents && <button type="button" className="new-events-button" onClick={returnToNewest}>New events</button>}
    </section>
  )
}

function EventRow({ event }: { event: ObserveEvent }) {
  const metadata = [event.component, event.kind, event.proxy, event.filter, event.protocol, event.direction].filter(Boolean)
  const timestamp = new Date(event.time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  return (
    <li className="event-row">
      <div className="event-primary">
        <time dateTime={event.time}>{timestamp}</time>
        <span className={`severity-badge severity-${event.level}`}>{event.level}</span>
        <p>{event.message}</p>
      </div>
      <p className="event-metadata">{metadata.join(' · ')}</p>
    </li>
  )
}

function EventSkeleton() {
  return <div className="event-skeleton" aria-label="Loading events">{Array.from({ length: 5 }, (_, index) => <span key={index} />)}</div>
}
