package engines

import (
	"context"
	"custom_erp/db"
	"fmt"
	"log"
	"time"
)

// Stage 35.3.7 - the reservation sweeper.
//
// Until now `expires_at` was written on every reservation and read by nothing,
// so every reservation was effectively permanent: an abandoned cart hold kept
// stock out of the sellable pool for ever.
//
// THE THING THIS DELIBERATELY DOES NOT DO: expire an order's reservation just
// because time has passed. `inventory.reservation_ttl_seconds` is a cart-hold
// TTL, and applying it to a confirmed order would release stock out from under
// a live order the moment it sat in a queue longer than the TTL - a far worse
// bug than the one being fixed. So the two halves of the sweep have different
// rules:
//
//	unattributed (cart/manual holds)  -> released when expires_at passes
//	attributed to an order            -> released only when the ORDER no longer
//	                                     needs it: cancelled, deleted, or its
//	                                     line has left a reserved state
//
// The second rule is what makes this safe to run continuously, and it also
// repairs a class of leak that existed before attribution: a line cancelled
// through a path that forgot to release now gets cleaned up on the next tick.

// ReservationSweepResult reports one sweep, so the worker can log something
// meaningful and a test can assert on it.
type ReservationSweepResult struct {
	ExpiredHolds     int `json:"expired_holds"`
	OrphanedOrderRes int `json:"orphaned_order_reservations"`
	QuantityReleased int `json:"quantity_released"`
}

// reservationSweepBatch bounds one pass. A tenant that has accumulated a large
// backlog is drained over several ticks rather than in one long transaction
// holding locks against the live order path.
const reservationSweepBatch = 500

// SweepExpiredReservations releases reservations nothing is waiting on. Safe to
// call repeatedly; it is a no-op when there is nothing to release.
func SweepExpiredReservations(tenantID string) (ReservationSweepResult, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return ReservationSweepResult{}, err
	}
	return sweepReservationsForSchema(schema)
}

func sweepReservationsForSchema(schema string) (ReservationSweepResult, error) {
	var result ReservationSweepResult

	tx, err := db.DB.Begin()
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if err := db.SetSearchPath(tx, schema); err != nil {
		return result, err
	}

	// One query, two rules. Written as a single statement so the sweep takes one
	// consistent snapshot: evaluating "is this order still live?" separately
	// from selecting the row is how a reservation gets released a millisecond
	// after the order that needed it was confirmed.
	//
	// The line-status test names the states that still hold stock. Anything else
	// - Cancelled, Returned, On Hold (which releases explicitly), or a line that
	// no longer exists - means the reservation is orphaned.
	rows, err := tx.Query(fmt.Sprintf(`
		SELECT r.id::text, r.sku, r.location_code, r.quantity, r.order_id IS NOT NULL
		FROM %s.inventory_reservation r
		WHERE
		    (r.order_id IS NULL AND r.expires_at < CURRENT_TIMESTAMP)
		 OR (r.order_id IS NOT NULL AND NOT EXISTS (
		        SELECT 1 FROM %s.documents l
		        WHERE l.doctype = 'SalesOrderLine'
		          AND l.id = r.line_id
		          AND l.deleted_at IS NULL
		          AND COALESCE(l.data->>'line_status', l.status) IN ('Reserved', 'Allocated', 'Picking', 'Picked', 'Packed')
		    ))
		ORDER BY r.created_at
		LIMIT %d
		FOR UPDATE SKIP LOCKED`, schema, schema, reservationSweepBatch))
	if err != nil {
		return result, err
	}

	type doomed struct {
		id, sku, location string
		quantity          int
		attributed        bool
	}
	var batch []doomed
	for rows.Next() {
		var row doomed
		if err := rows.Scan(&row.id, &row.sku, &row.location, &row.quantity, &row.attributed); err != nil {
			rows.Close()
			return result, err
		}
		batch = append(batch, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	rows.Close()
	if len(batch) == 0 {
		return result, tx.Commit()
	}

	for _, row := range batch {
		if _, err := tx.Exec(fmt.Sprintf(
			`DELETE FROM %s.inventory_reservation WHERE id = $1::uuid`, schema), row.id); err != nil {
			return result, err
		}
		// GREATEST(...,0) for the same reason releaseLineReservation uses it:
		// a historical inconsistency in the read model must not become a
		// negative reserved count, which would inflate ATS.
		if _, err := tx.Exec(fmt.Sprintf(
			`UPDATE %s.inventory_availability SET reserved = GREATEST(reserved - $1, 0), updated_at = CURRENT_TIMESTAMP
			 WHERE sku = $2 AND location_code = $3`, schema), row.quantity, row.sku, row.location); err != nil {
			return result, err
		}
		result.QuantityReleased += row.quantity
		if row.attributed {
			result.OrphanedOrderRes++
		} else {
			result.ExpiredHolds++
		}
	}
	return result, tx.Commit()
}

// StartReservationSweeper runs the sweep on every tenant schema. The interval
// wants to be short relative to the cart-hold TTL - stock released ten minutes
// after a cart was abandoned is stock nobody could sell for ten minutes - but
// not so short that it competes with the order path for locks. Five minutes is
// the balance the default TTL implies.
func StartReservationSweeper(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if db.DB == nil {
					continue
				}
				schemas, err := listTenantSchemas()
				if err != nil {
					log.Printf("[RESERVATION-SWEEP] Failed to list tenant schemas: %v", err)
					continue
				}
				for _, schema := range schemas {
					// A database that has not had the Stage 35.3.7 migration
					// applied has no order_id column, so the sweep query would
					// error every tick. Skip quietly instead.
					var ready bool
					if err := db.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.columns
						WHERE table_schema = $1 AND table_name = 'inventory_reservation' AND column_name = 'order_id')`, schema).Scan(&ready); err != nil || !ready {
						continue
					}
					result, err := sweepReservationsForSchema(schema)
					if err != nil {
						log.Printf("[RESERVATION-SWEEP] %s: %v", schema, err)
						continue
					}
					if result.ExpiredHolds > 0 || result.OrphanedOrderRes > 0 {
						log.Printf("[RESERVATION-SWEEP] %s: released %d expired hold(s) and %d orphaned order reservation(s), returning %d unit(s) to the sellable pool",
							schema, result.ExpiredHolds, result.OrphanedOrderRes, result.QuantityReleased)
					}
				}
			}
		}
	}()
}
