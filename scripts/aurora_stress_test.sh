#!/bin/bash
# Enhanced Aurora Stress Test Script
# This script runs stress tests on Aurora MySQL instances with a focus on generating audit logs
# Simplified version with fixed parameters: 20 threads, 20 tables, 500K rows

# Default values
MODE="run"
TARGET_INSTANCE="writer"
WORKLOAD_TYPE="oltp_read_write"
THREADS=20
TABLES=20
TABLE_SIZE=500000
DURATION=180
DB_USER="sysbench"
DB_PASSWORD="sysbench123"
DB_NAME="sysbench_test"
WRITER_ENDPOINT=""
READER_ENDPOINT=""
AUDIT_LOG_FOCUS=false
DDL_FREQUENCY=30  # Seconds between DDL operations
SCHEMA_CHANGES=true
USER_OPERATIONS=true

# Display help information
function show_help {
    echo "Enhanced Aurora Stress Test Script (Fixed Configuration)"
    echo "Usage: $0 [OPTIONS]"
    echo
    echo "Options:"
    echo "  --mode MODE                  Operation mode: setup, run, cleanup, all (default: run)"
    echo "  --target-instance TARGET     Target instance: writer, reader, both (default: writer)"
    echo "  --workload-type TYPE         Workload type: oltp_read_only, oltp_read_write, oltp_write_only, oltp_point_select,"
    echo "                               oltp_insert, oltp_update_index, oltp_update_non_index, oltp_delete,"
    echo "                               oltp_mixed, all (default: oltp_read_write)"
    echo "                               Can be comma-separated list to run multiple workloads"
    echo "  --duration N                 Test duration in seconds (default: 180)"
    echo "  --writer-endpoint HOST       Writer endpoint (required)"
    echo "  --reader-endpoint HOST       Reader endpoint (required for reader or both target)"
    echo "  --audit-log-focus            Focus on generating audit logs (enables additional operations)"
    echo "  --ddl-frequency N            Seconds between DDL operations when audit-log-focus is enabled (default: 30)"
    echo "  --no-schema-changes          Disable schema changes during audit log generation"
    echo "  --no-user-operations         Disable user operations during audit log generation"
    echo "  --help                       Show this help message"
    echo
    echo "Fixed Configuration:"
    echo "  Threads: 20"
    echo "  Tables: 20"
    echo "  Table Size: 500,000 rows"
    echo
    echo "Examples:"
    echo "  $0 --mode setup --writer-endpoint aurora-cluster.example.com"
    echo "  $0 --mode run --target-instance writer --writer-endpoint aurora-cluster.example.com"
    echo "  $0 --mode run --target-instance both --workload-type oltp_read_write,oltp_write_only --audit-log-focus --writer-endpoint aurora-cluster.example.com --reader-endpoint aurora-cluster-ro.example.com"
    echo "  $0 --mode all --audit-log-focus --writer-endpoint aurora-cluster.example.com"
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        --mode)
            MODE="$2"
            shift 2
            ;;
        --target-instance)
            TARGET_INSTANCE="$2"
            shift 2
            ;;
        --workload-type)
            WORKLOAD_TYPE="$2"
            shift 2
            ;;
        --duration)
            DURATION="$2"
            shift 2
            ;;
        --writer-endpoint)
            WRITER_ENDPOINT="$2"
            shift 2
            ;;
        --reader-endpoint)
            READER_ENDPOINT="$2"
            shift 2
            ;;
        --audit-log-focus)
            AUDIT_LOG_FOCUS=true
            shift
            ;;
        --ddl-frequency)
            DDL_FREQUENCY="$2"
            shift 2
            ;;
        --no-schema-changes)
            SCHEMA_CHANGES=false
            shift
            ;;
        --no-user-operations)
            USER_OPERATIONS=false
            shift
            ;;
        --help)
            show_help
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            show_help
            exit 1
            ;;
    esac
done

# Validate required parameters
if [[ -z "$WRITER_ENDPOINT" ]]; then
    echo "Error: Writer endpoint is required"
    show_help
    exit 1
fi

if [[ "$TARGET_INSTANCE" == "reader" || "$TARGET_INSTANCE" == "both" ]] && [[ -z "$READER_ENDPOINT" ]]; then
    echo "Error: Reader endpoint is required when target instance is reader or both"
    show_help
    exit 1
fi

# Function to setup the database
function setup_db {
    local endpoint=$1
    echo "Setting up database on $endpoint..."
    
    # Create test database and user (without trying to set audit logging variables)
    mysql -h $endpoint -u admin -pPassword123! << MYSQLEOF
CREATE DATABASE IF NOT EXISTS $DB_NAME;
CREATE USER IF NOT EXISTS '$DB_USER'@'%' IDENTIFIED BY '$DB_PASSWORD';
GRANT ALL PRIVILEGES ON $DB_NAME.* TO '$DB_USER'@'%';
FLUSH PRIVILEGES;
MYSQLEOF
    
    if [ $? -ne 0 ]; then
        echo "Error: Failed to create database and user"
        return 1
    fi
    
    # Try to enable audit logging, but don't fail if it doesn't work
    echo "Attempting to enable audit logging (may require SUPER privileges)..."
    mysql -h $endpoint -u admin -pPassword123! << MYSQLEOF 2>/dev/null || true
SET GLOBAL server_audit_events='CONNECT,QUERY,TABLE,QUERY_DDL,QUERY_DML,QUERY_DCL';
SET GLOBAL server_audit_logging=1;
MYSQLEOF
    
    # Check if audit logging is enabled
    AUDIT_STATUS=$(mysql -h $endpoint -u admin -pPassword123! -e "SHOW VARIABLES LIKE 'server_audit_logging';" 2>/dev/null | grep -c "ON" || echo "0")
    
    if [ "$AUDIT_STATUS" = "0" ]; then
        echo "Note: Could not enable audit logging directly. This requires SUPER privileges."
        echo "      Audit logs will still be generated if audit logging is enabled in the"
        echo "      Aurora parameter group. You can enable it through the AWS RDS console."
    else
        echo "Audit logging successfully enabled."
    fi
    
    # Prepare sysbench tables
    echo "Preparing sysbench tables with $TABLES tables of $TABLE_SIZE rows each..."
    sysbench oltp_read_write \
        --db-driver=mysql \
        --mysql-host=$endpoint \
        --mysql-user=$DB_USER \
        --mysql-password=$DB_PASSWORD \
        --mysql-db=$DB_NAME \
        --tables=$TABLES \
        --table-size=$TABLE_SIZE \
        --threads=$THREADS \
        prepare
    
    if [ $? -ne 0 ]; then
        echo "Error: Failed to prepare sysbench tables"
        return 1
    fi
    
    # Verify that all requested tables were created
    local existing_tables=$(mysql -h $endpoint -u $DB_USER -p$DB_PASSWORD -N -e "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='$DB_NAME' AND table_name LIKE 'sbtest%';" 2>/dev/null || echo "0")
    
    if [ "$existing_tables" -lt "$TABLES" ]; then
        echo "Warning: Only $existing_tables tables were created, but $TABLES were requested."
        echo "This may be due to sysbench limitations. Attempting to create remaining tables..."
        
        # Calculate how many more tables we need
        local tables_to_create=$((TABLES - existing_tables))
        
        # Create additional tables with a different table offset
        sysbench oltp_read_write \
            --db-driver=mysql \
            --mysql-host=$endpoint \
            --mysql-user=$DB_USER \
            --mysql-password=$DB_PASSWORD \
            --mysql-db=$DB_NAME \
            --tables=$tables_to_create \
            --table-size=$TABLE_SIZE \
            --threads=$THREADS \
            --table-offset=$existing_tables \
            prepare
            
        # Check again
        existing_tables=$(mysql -h $endpoint -u $DB_USER -p$DB_PASSWORD -N -e "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='$DB_NAME' AND table_name LIKE 'sbtest%';" 2>/dev/null || echo "0")
        echo "Now have $existing_tables tables out of $TABLES requested."
    fi
    
    # Create additional tables for audit log generation if audit log focus is enabled
    if [[ "$AUDIT_LOG_FOCUS" == true ]]; then
        echo "Creating additional tables for audit log generation..."
        for i in {1..10}; do
            mysql -h $endpoint -u $DB_USER -p$DB_PASSWORD $DB_NAME << MYSQLEOF
CREATE TABLE IF NOT EXISTS audit_test_$i (
    id INT AUTO_INCREMENT PRIMARY KEY,
    data VARCHAR(255),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);
MYSQLEOF
        done
    fi
    
    echo "Database setup completed successfully"
    return 0
}

# Function to run the stress test
function run_test {
    local endpoint=$1
    local workload=$2
    
    echo "Running $workload test on $endpoint..."
    echo "Parameters: threads=$THREADS, tables=$TABLES, duration=$DURATION seconds"
    
    # Check if we have enough tables for the test
    echo "Checking if required tables exist..."
    local existing_tables=$(mysql -h $endpoint -u $DB_USER -p$DB_PASSWORD -N -e "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='$DB_NAME' AND table_name LIKE 'sbtest%';" 2>/dev/null || echo "0")
    
    if [ "$existing_tables" -lt "$TABLES" ]; then
        echo "Warning: Only $existing_tables tables exist, but $TABLES are required."
        echo "Preparing additional tables..."
        
        # Prepare additional tables
        sysbench oltp_read_write \
            --db-driver=mysql \
            --mysql-host=$endpoint \
            --mysql-user=$DB_USER \
            --mysql-password=$DB_PASSWORD \
            --mysql-db=$DB_NAME \
            --tables=$TABLES \
            --table-size=$TABLE_SIZE \
            --threads=$THREADS \
            prepare
        
        if [ $? -ne 0 ]; then
            echo "Error: Failed to prepare additional tables"
            return 1
        fi
        
        echo "Additional tables prepared successfully."
    fi
    
    # Start audit log generation in background if enabled
    if [[ "$AUDIT_LOG_FOCUS" == true ]]; then
        generate_audit_logs $endpoint &
        AUDIT_PID=$!
        echo "Started audit log generation with PID: $AUDIT_PID"
    fi
    
    # Run the sysbench test
    sysbench $workload \
        --db-driver=mysql \
        --mysql-host=$endpoint \
        --mysql-user=$DB_USER \
        --mysql-password=$DB_PASSWORD \
        --mysql-db=$DB_NAME \
        --tables=$TABLES \
        --table-size=$TABLE_SIZE \
        --threads=$THREADS \
        --time=$DURATION \
        run
    
    TEST_RESULT=$?
    
    # Stop audit log generation if it was started
    if [[ "$AUDIT_LOG_FOCUS" == true && -n "$AUDIT_PID" ]]; then
        echo "Stopping audit log generation (PID: $AUDIT_PID)..."
        kill $AUDIT_PID 2>/dev/null
        wait $AUDIT_PID 2>/dev/null
        echo "Audit log generation stopped"
    fi
    
    if [ $TEST_RESULT -ne 0 ]; then
        echo "Error: Test failed"
        return 1
    fi
    
    echo "Test completed successfully"
    return 0
}

# Function to generate audit logs
function generate_audit_logs {
    local endpoint=$1
    local counter=0
    
    echo "Starting audit log generation on $endpoint..."
    
    # Create a temporary file for SQL commands
    TEMP_SQL_FILE=$(mktemp)
    
    # Continue until the process is killed
    while true; do
        counter=$((counter + 1))
        
        # Schema changes (DDL operations)
        if [[ "$SCHEMA_CHANGES" == true && $((counter % DDL_FREQUENCY)) -eq 0 ]]; then
            echo "Executing schema changes (DDL operations)..."
            
            # Generate random table name
            TABLE_SUFFIX=$RANDOM
            
            # Create a new table
            cat > $TEMP_SQL_FILE << EOSQL
-- Create a new table
CREATE TABLE IF NOT EXISTS audit_ddl_test_$TABLE_SUFFIX (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(100),
    value INT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Add an index
CREATE INDEX idx_name_$TABLE_SUFFIX ON audit_ddl_test_$TABLE_SUFFIX(name);

-- Alter the table
ALTER TABLE audit_ddl_test_$TABLE_SUFFIX ADD COLUMN description TEXT;

-- Drop the index
DROP INDEX idx_name_$TABLE_SUFFIX ON audit_ddl_test_$TABLE_SUFFIX;

-- Drop the table
DROP TABLE IF EXISTS audit_ddl_test_$TABLE_SUFFIX;
EOSQL

            # Execute the DDL operations
            mysql -h $endpoint -u $DB_USER -p$DB_PASSWORD $DB_NAME < $TEMP_SQL_FILE
        fi
        
        # User operations (DCL operations)
        if [[ "$USER_OPERATIONS" == true && $((counter % (DDL_FREQUENCY * 2))) -eq 0 ]]; then
            echo "Executing user operations (DCL operations)..."
            
            # Generate random user name
            USER_SUFFIX=$RANDOM
            
            # Create and manage users
            cat > $TEMP_SQL_FILE << EOSQL
-- Create a new user
CREATE USER IF NOT EXISTS 'audit_test_user_$USER_SUFFIX'@'%' IDENTIFIED BY 'password$USER_SUFFIX';

-- Grant privileges
GRANT SELECT, INSERT ON $DB_NAME.* TO 'audit_test_user_$USER_SUFFIX'@'%';

-- Show grants
SHOW GRANTS FOR 'audit_test_user_$USER_SUFFIX'@'%';

-- Revoke privileges
REVOKE INSERT ON $DB_NAME.* FROM 'audit_test_user_$USER_SUFFIX'@'%';

-- Drop user
DROP USER IF EXISTS 'audit_test_user_$USER_SUFFIX'@'%';
EOSQL

            # Execute the DCL operations
            mysql -h $endpoint -u admin -pPassword123! < $TEMP_SQL_FILE
        fi
        
        # DML operations on audit test tables
        for i in {1..10}; do
            # Insert, update, and delete operations
            cat > $TEMP_SQL_FILE << EOSQL
-- Insert data
INSERT INTO audit_test_$i (data) VALUES ('test_data_$counter');

-- Update data
UPDATE audit_test_$i SET data = CONCAT(data, '_updated') WHERE id = LAST_INSERT_ID();

-- Select data
SELECT * FROM audit_test_$i WHERE id = LAST_INSERT_ID();

-- Delete data (occasionally)
DELETE FROM audit_test_$i WHERE id = LAST_INSERT_ID() AND $counter % 5 = 0;
EOSQL

            # Execute the DML operations
            mysql -h $endpoint -u $DB_USER -p$DB_PASSWORD $DB_NAME < $TEMP_SQL_FILE
        done
        
        # Sleep briefly to avoid overwhelming the database
        sleep 1
    done
    
    # Clean up
    rm -f $TEMP_SQL_FILE
}

# Function to monitor audit log size
function monitor_audit_logs {
    local endpoint=$1
    local interval=30  # Check every 30 seconds
    
    echo "Starting audit log monitoring on $endpoint..."
    
    while true; do
        # Get the size of audit logs
        LOG_INFO=$(mysql -h $endpoint -u admin -pPassword123! -e "SHOW VARIABLES LIKE 'server_audit_file_path';" | grep server_audit_file_path)
        LOG_PATH=$(echo $LOG_INFO | awk '{print $2}')
        
        echo "$(date): Current audit log path: $LOG_PATH"
        echo "$(date): Audit log files:"
        
        # List audit log files and their sizes
        mysql -h $endpoint -u admin -pPassword123! -e "SELECT file_name, file_size FROM information_schema.files WHERE file_name LIKE '%audit%';"
        
        sleep $interval
    done
}

# Function to cleanup the database
function cleanup_db {
    local endpoint=$1
    echo "Cleaning up database on $endpoint..."
    
    # Cleanup sysbench tables
    sysbench oltp_read_write \
        --db-driver=mysql \
        --mysql-host=$endpoint \
        --mysql-user=$DB_USER \
        --mysql-password=$DB_PASSWORD \
        --mysql-db=$DB_NAME \
        --tables=$TABLES \
        cleanup
    
    if [ $? -ne 0 ]; then
        echo "Error: Failed to cleanup sysbench tables"
        return 1
    fi
    
    # Drop additional audit test tables if they exist
    if [[ "$AUDIT_LOG_FOCUS" == true ]]; then
        echo "Dropping audit test tables..."
        for i in {1..10}; do
            mysql -h $endpoint -u $DB_USER -p$DB_PASSWORD $DB_NAME -e "DROP TABLE IF EXISTS audit_test_$i;"
        done
    fi
    
    # Drop database and user
    mysql -h $endpoint -u admin -pPassword123! << MYSQLEOF
DROP DATABASE IF EXISTS $DB_NAME;
DROP USER IF EXISTS '$DB_USER'@'%';
MYSQLEOF
    
    if [ $? -ne 0 ]; then
        echo "Error: Failed to drop database and user"
        return 1
    fi
    
    echo "Cleanup completed successfully"
    return 0
}

# Main execution
echo "Enhanced Aurora Stress Test (Fixed Configuration)"
echo "=========================="
echo "Mode: $MODE"
echo "Target Instance: $TARGET_INSTANCE"
echo "Workload Type: $WORKLOAD_TYPE"
echo "Threads: $THREADS"
echo "Tables: $TABLES"
echo "Table Size: $TABLE_SIZE"
echo "Duration: $DURATION seconds"
echo "Writer Endpoint: $WRITER_ENDPOINT"
echo "Reader Endpoint: $READER_ENDPOINT"
echo "Audit Log Focus: $AUDIT_LOG_FOCUS"
if [[ "$AUDIT_LOG_FOCUS" == true ]]; then
    echo "DDL Frequency: Every $DDL_FREQUENCY seconds"
    echo "Schema Changes: $SCHEMA_CHANGES"
    echo "User Operations: $USER_OPERATIONS"
fi
echo "=========================="

# Execute based on mode
case "$MODE" in
    setup)
        setup_db $WRITER_ENDPOINT
        ;;
    run)
        # Split workload types by comma and run each one
        IFS=',' read -ra WORKLOAD_TYPES <<< "$WORKLOAD_TYPE"
        
        for wt in "${WORKLOAD_TYPES[@]}"; do
            if [[ "$wt" == "all" ]]; then
                # Run all workload types
                WORKLOADS=("oltp_read_write" "oltp_read_only" "oltp_write_only" "oltp_point_select" "oltp_insert" "oltp_update_index" "oltp_update_non_index" "oltp_delete")
                for w in "${WORKLOADS[@]}"; do
                    if [[ "$TARGET_INSTANCE" == "writer" || "$TARGET_INSTANCE" == "both" ]]; then
                        echo "Running $w test on writer instance..."
                        run_test $WRITER_ENDPOINT $w
                    fi
                    
                    if [[ "$TARGET_INSTANCE" == "reader" || "$TARGET_INSTANCE" == "both" ]]; then
                        echo "Running $w test on reader instance..."
                        run_test $READER_ENDPOINT $w
                    fi
                done
            else
                if [[ "$TARGET_INSTANCE" == "writer" || "$TARGET_INSTANCE" == "both" ]]; then
                    echo "Running $wt test on writer instance..."
                    run_test $WRITER_ENDPOINT $wt
                fi
                
                if [[ "$TARGET_INSTANCE" == "reader" || "$TARGET_INSTANCE" == "both" ]]; then
                    echo "Running $wt test on reader instance..."
                    run_test $READER_ENDPOINT $wt
                fi
            fi
        done
        ;;
    cleanup)
        cleanup_db $WRITER_ENDPOINT
        ;;
    all)
        setup_db $WRITER_ENDPOINT
        
        # Split workload types by comma and run each one
        IFS=',' read -ra WORKLOAD_TYPES <<< "$WORKLOAD_TYPE"
        
        for wt in "${WORKLOAD_TYPES[@]}"; do
            if [[ "$wt" == "all" ]]; then
                # Run all workload types
                WORKLOADS=("oltp_read_write" "oltp_read_only" "oltp_write_only" "oltp_point_select" "oltp_insert" "oltp_update_index" "oltp_update_non_index" "oltp_delete")
                for w in "${WORKLOADS[@]}"; do
                    if [[ "$TARGET_INSTANCE" == "writer" || "$TARGET_INSTANCE" == "both" ]]; then
                        echo "Running $w test on writer instance..."
                        run_test $WRITER_ENDPOINT $w
                    fi
                    
                    if [[ "$TARGET_INSTANCE" == "reader" || "$TARGET_INSTANCE" == "both" ]]; then
                        echo "Running $w test on reader instance..."
                        run_test $READER_ENDPOINT $w
                    fi
                done
            else
                if [[ "$TARGET_INSTANCE" == "writer" || "$TARGET_INSTANCE" == "both" ]]; then
                    echo "Running $wt test on writer instance..."
                    run_test $WRITER_ENDPOINT $wt
                fi
                
                if [[ "$TARGET_INSTANCE" == "reader" || "$TARGET_INSTANCE" == "both" ]]; then
                    echo "Running $wt test on reader instance..."
                    run_test $READER_ENDPOINT $wt
                fi
            fi
        done
        
        cleanup_db $WRITER_ENDPOINT
        ;;
    *)
        echo "Error: Invalid mode '$MODE'"
        show_help
        exit 1
        ;;
esac

echo "Enhanced Aurora Stress Test completed"
exit 0