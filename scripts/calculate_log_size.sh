#!/bin/sh

# Set your DB instance identifier
DB_INSTANCE="tf-2025070210432600100000000a"

# Get log files information using AWS CLI with text output
echo "Fetching log files information..."
aws rds describe-db-log-files --db-instance-identifier "$DB_INSTANCE" --output text > /tmp/rds_logs.txt

# Show sample of the data
echo "Sample of raw data:"
head -3 /tmp/rds_logs.txt

echo "Calculating log sizes by hour..."

# First pass: Extract unique hour keys and create temporary files for each hour
echo "Extracting hour information..."
cat /tmp/rds_logs.txt | while read -r line; do
    # Fields are: DESCRIBEDBLOGFILES timestamp filename size
    desc=$(echo "$line" | awk '{print $1}')
    timestamp=$(echo "$line" | awk '{print $2}')
    filename=$(echo "$line" | awk '{print $3}')
    size=$(echo "$line" | awk '{print $4}')
    
    # Extract date and hour using grep and cut
    date_hour=$(echo "$filename" | grep -o '[0-9]\{4\}-[0-9]\{2\}-[0-9]\{2\}-[0-9]\{2\}')
    
    if [ -n "$date_hour" ]; then
        # Split into date and hour
        date=$(echo "$date_hour" | cut -d'-' -f1,2,3)
        hour=$(echo "$date_hour" | cut -d'-' -f4)
        
        hour_key="${date}_${hour}"
        
        # Append size to the appropriate hour file
        echo "$size" >> "/tmp/hour_${hour_key}.txt"
    else
        echo "Unmatched filename: $filename"
    fi
done

# Second pass: Calculate totals for each hour
echo ""
echo "Hourly breakdown:"
echo "Date Hour, Size (bytes), Size (human readable)"
echo "----------------------------------------"

# Get list of hour files
hour_files=$(ls /tmp/hour_*.txt 2>/dev/null)

if [ -z "$hour_files" ]; then
    echo "No log files matched the date/hour patterns!"
else
    for hour_file in $hour_files; do
        # Extract hour key from filename
        hour_key=$(echo "$hour_file" | sed 's/\/tmp\/hour_\(.*\)\.txt/\1/')
        
        # Replace underscore with space and add :00
        display_key=$(echo "$hour_key" | sed 's/_/ /g')":00"
        
        # Calculate total size for this hour
        total=0
        while read -r size; do
            total=$((total + size))
        done < "$hour_file"
        
        # Format size for human readability
        if [ $total -ge 1048576 ]; then
            human_size=$(echo "scale=2; $total/1048576" | bc)" MB"
        elif [ $total -ge 1024 ]; then
            human_size=$(echo "scale=2; $total/1024" | bc)" KB"
        else
            human_size="$total bytes"
        fi
        
        printf "%-20s %12d %20s\n" "$display_key" "$total" "$human_size"
    done
fi

# Clean up
rm -f /tmp/rds_logs.txt /tmp/hour_*.txt

echo "Done!"

