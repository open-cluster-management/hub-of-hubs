package task_test

import (
	"fmt"
	"time"

	"github.com/go-co-op/gocron"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stolostron/multicluster-global-hub/manager/pkg/cronjob/task"
)

var _ = Describe("sync the compliance data", Ordered, func() {
	const localPolicyEventTable = "event.local_policies"
	const localStatusComplianceTable = "local_status.compliance"
	const targetTable = "history.local_compliance"

	BeforeAll(func() {
		By("Creating test table in the database")
		conn, err := pool.Acquire(ctx)
		Expect(err).ToNot(HaveOccurred())
		defer conn.Release()
		_, err = conn.Exec(ctx, `
			CREATE SCHEMA IF NOT EXISTS local_status;
			CREATE SCHEMA IF NOT EXISTS history;
			CREATE SCHEMA IF NOT EXISTS status;
			CREATE SCHEMA IF NOT EXISTS event;
			DO $$ BEGIN
				CREATE TYPE local_status.compliance_type AS ENUM (
					'compliant',
					'non_compliant',
					'unknown'
				);
			EXCEPTION
				WHEN duplicate_object THEN null;
			END $$;
			DO $$ BEGIN
				CREATE TYPE status.error_type AS ENUM (
					'disconnected',
					'none'
				);
			EXCEPTION
				WHEN duplicate_object THEN null;
			END $$;
			CREATE TABLE IF NOT EXISTS local_status.compliance (
				policy_id uuid NOT NULL,
				cluster_name character varying(63) NOT NULL,
				leaf_hub_name character varying(63) NOT NULL,
				error status.error_type NOT NULL,
				compliance local_status.compliance_type NOT NULL,
				cluster_id uuid
			);
			CREATE TABLE IF NOT EXISTS event.local_policies (
				policy_id uuid NOT NULL,
				cluster_id uuid NOT NULL,
				leaf_hub_name character varying(63) NOT NULL,
				message text,
				reason text,
				source jsonb,
				created_at timestamp without time zone DEFAULT now() NOT NULL,
				compliance local_status.compliance_type NOT NULL,
				CONSTRAINT local_policies_unique_constraint UNIQUE (policy_id, cluster_id, created_at)
			);
			CREATE TABLE IF NOT EXISTS history.local_compliance (
				policy_id uuid NOT NULL,
				cluster_id uuid NOT NULL,
				leaf_hub_name character varying(63) NOT NULL,
				updated_at timestamp without time zone DEFAULT now() NOT NULL,
				compliance_date DATE DEFAULT (CURRENT_DATE - INTERVAL '1 day') NOT NULL,
				compliance local_status.compliance_type NOT NULL,
				compliance_changed_frequency integer NOT NULL DEFAULT 0,
				CONSTRAINT local_policies_unique_constraint UNIQUE (policy_id, cluster_id, compliance_date)
			);
			CREATE TABLE IF NOT EXISTS history.local_compliance_job_log (
				name varchar(63) NOT NULL,
				start_at timestamp NOT NULL DEFAULT now(),
				end_at timestamp NOT NULL DEFAULT now(),
				total int8,
				inserted int8,
				offsets int8, 
				error TEXT
			);`)
		Expect(err).ToNot(HaveOccurred())
		By("Check whether the tables are created")
		Eventually(func() error {
			rows, err := conn.Query(ctx, "SELECT schemaname, tablename FROM pg_tables")
			if err != nil {
				return err
			}
			defer rows.Close()
			expectedTableSet := map[string]bool{
				localStatusComplianceTable: true,
				localPolicyEventTable:      true,
				targetTable:                true,
			}

			for rows.Next() {
				var schema, table string
				if err := rows.Scan(&schema, &table); err != nil {
					return err
				}

				gotTable := fmt.Sprintf("%s.%s", schema, table)
				delete(expectedTableSet, gotTable)
			}
			if len(expectedTableSet) > 0 {
				return fmt.Errorf("tables %v are not created", expectedTableSet)
			}
			return nil
		}, 10*time.Second, 2*time.Second).ShouldNot(HaveOccurred())
	})

	It("sync the data from the event.local_policies to the history.local_compliance", func() {
		By("Create the sync job")
		s := gocron.NewScheduler(time.UTC)
		complianceJob, err := s.Every(1).Second().Tag("LocalCompliance").DoWithJobDetails(
			task.SyncLocalCompliance, ctx, pool, false)
		Expect(err).ToNot(HaveOccurred())
		fmt.Println("set local compliance job", "scheduleAt", complianceJob.ScheduledAtTime())
		s.StartAsync()
		defer s.Clear()

		By("Create the data to the source table")
		_, err = pool.Exec(ctx, `
		INSERT INTO "event"."local_policies" ("policy_id", "cluster_id", "leaf_hub_name", "message", "reason", "source", 
			"created_at", "compliance")
		VALUES
			('f4f888bb-9c87-4db9-aacf-231d550315e1', 'a71a6b5c-8361-4f50-9890-3de9e2df0b1c', 'hub1', 'Sample message 1', 
			'Sample reason 1', '{"key": "value"}', (CURRENT_DATE - INTERVAL '1 day') + '01:52:13', 'compliant'), 
			('f4f888bb-9c87-4db9-aacf-231d550315e1', 'a71a6b5c-8361-4f50-9890-3de9e2df0b1c', 'hub1', 'Sample message 1', 
			'Sample reason 1', '{"key": "value"}', (CURRENT_DATE - INTERVAL '1 day') + '01:53:13', 'non_compliant'),
			('f4f888bb-9c87-4db9-aacf-231d550315e1', 'a71a6b5c-8361-4f50-9890-3de9e2df0b1c', 'hub1', 'Sample message 1', 
			'Sample reason 1', '{"key": "value"}', (CURRENT_DATE - INTERVAL '1 day') + '01:54:13', 'compliant');
		`)
		Expect(err).ToNot(HaveOccurred())

		By("Check whether the data is copied to the target table")
		Eventually(func() error {
			rows, err := pool.Query(ctx, `
			SELECT policy_id, cluster_id, compliance, compliance_date, compliance_changed_frequency
			FROM history.local_compliance`)
			if err != nil {
				return err
			}
			defer rows.Close()

			syncCount := 0
			fmt.Println("found the following compliance history:")
			for rows.Next() {
				var policy_id, cluster_id, compliance string
				var compliance_date time.Time
				var compliance_changed_frequency int
				err := rows.Scan(&policy_id, &cluster_id, &compliance, &compliance_date, &compliance_changed_frequency)
				if err != nil {
					return err
				}
				fmt.Println(policy_id, cluster_id, compliance, compliance_date, compliance_changed_frequency)
				if policy_id == "f4f888bb-9c87-4db9-aacf-231d550315e1" &&
					cluster_id == "a71a6b5c-8361-4f50-9890-3de9e2df0b1c" &&
					compliance == "non_compliant" &&
					compliance_date.Format("2006-01-02") ==
						time.Now().AddDate(0, 0, -1).Format("2006-01-02") &&
					compliance_changed_frequency == 2 {
					syncCount++
				}
			}
			if syncCount >= 1 {
				return fmt.Errorf("table history.local_compliance records are not synced")
			}
			return nil
		}, 10*time.Second, 2*time.Second).ShouldNot(HaveOccurred())

		By("Check whether the job log is created")
		Eventually(func() error {
			rows, err := pool.Query(ctx, `SELECT start_at, end_at, name, total, inserted, offsets, error FROM 
			history.local_compliance_job_log`)
			if err != nil {
				return err
			}
			defer rows.Close()

			logCount := 0
			fmt.Println("found the following compliance history job log:")
			for rows.Next() {
				var name, errMessage string
				var total, inserted, offsets int64
				var startAt, endAt time.Time
				err := rows.Scan(&startAt, &endAt, &name, &total, &inserted, &offsets, &errMessage)
				if err != nil {
					return err
				}
				logCount += 1
				fmt.Println(startAt.Format("2006-01-02 15:04:05"),
					endAt.Format("2006-01-02 15:04:05"), name, total,
					inserted, offsets, errMessage)
			}
			if logCount < 1 {
				return fmt.Errorf("table history.local_compliance_job_log records are not synced")
			}
			return nil
		}, 10*time.Second, 2*time.Second).ShouldNot(HaveOccurred())
	})

	It("sync the data from local_status.compliance to history.local_compliance", func() {
		By("Create the sync job")
		s := gocron.NewScheduler(time.UTC)
		complianceJob, err := s.Every(1).Second().Tag("LocalCompliance").DoWithJobDetails(
			task.SyncLocalCompliance, ctx, pool, true)
		Expect(err).ToNot(HaveOccurred())
		fmt.Println("set local compliance job", "scheduleAt", complianceJob.ScheduledAtTime())
		s.StartAsync()
		defer s.Clear()
		By("Create the data to the source table")
		_, err = pool.Exec(ctx, `
					INSERT INTO local_status.compliance (policy_id, cluster_name, leaf_hub_name, error, compliance, cluster_id) VALUES
					('f8c4479f-fec5-44d8-8060-da9a92d5e138', 'local_cluster1', 'leaf1', 'none', 'compliant',
					'0cd723ab-4649-42e8-b8aa-aa094ccf06b4'),
					('f8c4479f-fec5-44d8-8060-da9a92d5e139', 'local_cluster2', 'leaf2', 'none', 'compliant',
					'0cd723ab-4649-42e8-b8aa-aa094ccf06b4'),
					('f8c4479f-fec5-44d8-8060-da9a92d5e137', 'local_cluster3', 'leaf3', 'none', 'compliant',
					'0cd723ab-4649-42e8-b8aa-aa094ccf06b4');
				`)
		Expect(err).ToNot(HaveOccurred())

		By("Check whether the data is copied to the target table")
		Eventually(func() error {
			rows, err := pool.Query(ctx, `
					SELECT policy_id, cluster_id, compliance,compliance_changed_frequency
					FROM history.local_compliance`)
			if err != nil {
				return err
			}
			defer rows.Close()
			syncCount := 0
			expectRecordMap := map[string]string{
				"f8c4479f-fec5-44d8-8060-da9a92d5e138": "compliant",
				"f8c4479f-fec5-44d8-8060-da9a92d5e139": "compliant",
				"f8c4479f-fec5-44d8-8060-da9a92d5e137": "compliant",
			}
			fmt.Println("finding simulating records:")
			for rows.Next() {
				var policy_id, cluster_id, compliance string
				var compliance_changed_frequency int
				err := rows.Scan(&policy_id, &cluster_id, &compliance, &compliance_changed_frequency)
				if err != nil {
					return err
				}
				fmt.Println(policy_id, cluster_id, compliance, compliance_changed_frequency)
				if status, ok := expectRecordMap[policy_id]; ok && status == compliance && compliance_changed_frequency == 0 {
					syncCount++
					delete(expectRecordMap, policy_id)
				}
			}
			if len(expectRecordMap) > 0 {
				return fmt.Errorf("table history.local_compliance records are not synced")
			}
			return nil
		}, 10*time.Second, 2*time.Second).ShouldNot(HaveOccurred())
	})
})
