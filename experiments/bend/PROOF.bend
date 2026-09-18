import Base
import ./LAWS.bend as Laws

def Laws.outbound_requires_triage(state):
  {==}

def Laws.snoozed_is_silent(recent, state, fires, overdue):
  {==}

def Laws.cooldown_is_silent(snoozed, state, fires, overdue):
  match snoozed:
    case True{}:
      {==}
    case False{}:
      {==}

def Laws.unclassified_is_silent(snoozed, recent, fires, overdue):
  match snoozed:
    case True{}:
      {==}
    case False{}:
      match recent:
        case True{}:
          {==}
        case False{}:
          {==}

def Laws.done_is_silent(snoozed, recent, fires, overdue):
  match snoozed:
    case True{}:
      {==}
    case False{}:
      match recent:
        case True{}:
          {==}
        case False{}:
          {==}

def Laws.overdue_request_can_nudge(fires):
  {==}

def Laws.reply_limit_is_silent(extra, overdue):
  {==}

def Laws.first_reply_can_nudge():
  {==}

def Laws.second_reply_can_nudge():
  {==}

def Laws.quiet_batch_is_silent(fired, candidates):
  {==}

def Laws.exhausted_batch_is_silent(extra, candidates):
  match candidates:
    case 0n:
      {==}
    case 1n+rest:
      {==}

def Laws.full_allowance_is_used():
  {==}

def Laws.failed_delivery_stays_stopped(confirmed, succeeded):
  {==}

def Laws.failed_delivery_is_not_counted(confirmed):
  {==}

def Laws.successful_delivery_is_counted(confirmed):
  {==}

def Laws.skipped_delivery_is_unchanged(state, succeeded):
  {==}

def Laws.batch_within_allowance(fired, candidates):
  match fired candidates:
    case 0n 0n:
      {==}
    case 0n 1n:
      {==}
    case 0n 2n:
      {==}
    case 0n 3n:
      {==}
    case 0n 4n:
      {==}
    case 0n 5n:
      {==}
    case 0n 6n+rest:
      {==}
    case 1n 0n:
      {==}
    case 1n 1n:
      {==}
    case 1n 2n:
      {==}
    case 1n 3n:
      {==}
    case 1n 4n:
      {==}
    case 1n 5n+rest:
      {==}
    case 2n 0n:
      {==}
    case 2n 1n:
      {==}
    case 2n 2n:
      {==}
    case 2n 3n:
      {==}
    case 2n 4n+rest:
      {==}
    case 3n 0n:
      {==}
    case 3n 1n:
      {==}
    case 3n 2n:
      {==}
    case 3n 3n+rest:
      {==}
    case 4n 0n:
      {==}
    case 4n 1n:
      {==}
    case 4n 2n+rest:
      {==}
    case 5n 0n:
      {==}
    case 5n 1n+rest:
      {==}
    case 6n+extra 0n:
      {==}
    case 6n+extra 1n+rest:
      {==}
