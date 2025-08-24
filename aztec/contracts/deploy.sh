#!/bin/bash

# Aztec Deployment Wizard Script
# Interactive deployment script for Token and Wormhole contracts on Aztec testnet

set -e  # Exit on any error

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
MAGENTA='\033[0;35m'
NC='\033[0m' # No Color

# Configuration variables with defaults
DEFAULT_NODE_URL="https://aztec-alpha-testnet-fullnode.zkv.xyz"
DEFAULT_SPONSORED_FPC_ADDRESS="0x19b5539ca1b104d4c3705de94e4555c9630def411f025e023a13189d0c56f8f22"
DEFAULT_OWNER_SK="0x0ff5c4c050588f4614255a5a4f800215b473e442ae9984347b3a727c3bb7ca55"

# Actual configuration (will be set by wizard)
NODE_URL=""
SPONSORED_FPC_ADDRESS=""
OWNER_SK=""

# Contract addresses (captured during deployment)
OWNER_ADDRESS=""
RECEIVER_ADDRESS=""
TOKEN_CONTRACT_ADDRESS=""
WORMHOLE_CONTRACT_ADDRESS=""

# Contract file paths
WORMHOLE_CONTRACT_SRC="src/main.nr"
WORMHOLE_CONTRACT_BACKUP="src/main.nr.backup"

# Logging functions
log() {
    echo -e "${BLUE}[$(date +'%Y-%m-%d %H:%M:%S')]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1" >&2
}

success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

info() {
    echo -e "${CYAN}[INFO]${NC} $1"
}

wizard_header() {
    echo -e "${MAGENTA}"
    echo "╔══════════════════════════════════════════════════════════╗"
    echo "║                  AZTEC DEPLOYMENT WIZARD                ║"
    echo "║              Token & Wormhole Contract Deployer         ║"
    echo "╚══════════════════════════════════════════════════════════╝"
    echo -e "${NC}"
}

# Wizard configuration function
setup_wizard() {
    wizard_header
    
    echo -e "\n${CYAN}Welcome to the Aztec Deployment Wizard!${NC}\n"
    
    echo "This wizard will help you deploy Token and Wormhole contracts on Aztec testnet."
    echo "You can use default values or customize the configuration."
    echo ""
    
    read -p "Do you want to use default configuration values? (y/n): " use_defaults
    
    if [[ $use_defaults =~ ^[Yy]$ ]]; then
        NODE_URL="$DEFAULT_NODE_URL"
        SPONSORED_FPC_ADDRESS="$DEFAULT_SPONSORED_FPC_ADDRESS"
        OWNER_SK="$DEFAULT_OWNER_SK"
        
        success "Using default configuration"
    else
        echo -e "\n${CYAN}Let's configure your deployment settings:${NC}\n"
        
        # Node URL configuration
        echo -e "${YELLOW}Node URL Configuration:${NC}"
        echo "Default: $DEFAULT_NODE_URL"
        read -p "Enter Node URL (press Enter for default): " input_node_url
        NODE_URL="${input_node_url:-$DEFAULT_NODE_URL}"
        
        # Sponsored FPC Address configuration
        echo -e "\n${YELLOW}Sponsored FPC Address Configuration:${NC}"
        echo "Default: $DEFAULT_SPONSORED_FPC_ADDRESS"
        read -p "Enter Sponsored FPC Address (press Enter for default): " input_fpc_address
        SPONSORED_FPC_ADDRESS="${input_fpc_address:-$DEFAULT_SPONSORED_FPC_ADDRESS}"
        
        # Owner Private Key configuration
        echo -e "\n${YELLOW}Owner Private Key Configuration:${NC}"
        echo "Default: $DEFAULT_OWNER_SK"
        read -p "Enter Owner Private Key (press Enter for default): " input_owner_sk
        OWNER_SK="${input_owner_sk:-$DEFAULT_OWNER_SK}"
        
        success "Custom configuration set"
    fi
    
    echo -e "\n${CYAN}Configuration Summary:${NC}"
    echo "Node URL: $NODE_URL"
    echo "FPC Address: $SPONSORED_FPC_ADDRESS"
    echo "Owner SK: ${OWNER_SK:0:10}..."
    echo ""
    
    warning "Note: If using the default private key, account deployment may fail with 'Existing nullifier' error if already deployed"
    info "This is expected behavior when the same key is reused"
    echo ""
    
    read -p "Press Enter to continue with deployment..."
}

# Check dependencies
check_dependencies() {
    log "Checking dependencies..."
    
    local missing_deps=()
    
    if ! command -v aztec-wallet &> /dev/null; then
        missing_deps+=("aztec-wallet")
    fi
    
    if ! command -v aztec-nargo &> /dev/null; then
        missing_deps+=("aztec-nargo")
    fi
    
    if ! command -v aztec &> /dev/null; then
        missing_deps+=("aztec (for testing)")
    fi
    
    if [ ${#missing_deps[@]} -ne 0 ]; then
        error "Missing dependencies: ${missing_deps[*]}"
        error "Please install Aztec CLI tools before continuing."
        exit 1
    fi
    
    success "All dependencies are installed"
}

# Extract transaction ID from output
extract_transaction_id() {
    local output="$1"
    echo "$output" | grep "Transaction hash:" | head -1 | sed 's/Transaction hash: //' | tr -d ' '
}

# Extract address using a flexible pattern
extract_address() {
    local output="$1"
    local pattern="${2:-0x[a-fA-F0-9]{64}}"
    echo "$output" | grep -o "$pattern" | head -1
}

# Check and handle stale transactions
check_and_handle_stale_transaction() {
    local tx_id="$1"
    local description="$2"
    
    warning "Transaction may be stale: $tx_id"
    info "You can check transaction status at: http://aztecscan.xyz/tx/$tx_id"
    
    echo ""
    echo "What would you like to do?"
    echo "1. Retry deployment (recommended)"
    echo "2. Wait and continue (if you think transaction will complete)"
    echo "3. Skip this step (not recommended)"
    
    local choice
    read -p "Choose option (1/2/3) [default: 1]: " choice
    choice=${choice:-1}
    
    case $choice in
        1)
            info "Will retry deployment"
            return 0  # Retry
            ;;
        2)
            info "Continuing with potentially stale transaction"
            return 1  # Continue
            ;;
        3)
            warning "Skipping deployment step"
            return 2  # Skip
            ;;
        *)
            info "Invalid choice, defaulting to retry"
            return 0  # Retry
            ;;
    esac
}

# Wait for transaction to be mined with retry logic
wait_for_transaction() {
    local description="$1"
    local max_attempts=30  # 10 minutes with 20-second intervals
    local attempt=0
    
    log "Waiting for $description to be mined (this may take up to 10 minutes)..."
    log "You can check transaction status at: http://aztecscan.xyz/"
    
    while [ $attempt -lt $max_attempts ]; do
        sleep 20
        attempt=$((attempt + 1))
        info "Waiting... (attempt $attempt/$max_attempts) - Check http://aztecscan.xyz/ for status"
        
        # You can add specific transaction checking logic here if needed
        # For now, we'll just wait and let the next command handle the verification
    done
    
    warning "Maximum wait time reached. Proceeding with next step..."
    info "If deployment is still pending, you can check http://aztecscan.xyz/"
}

# Execute command with retry logic and specific error handling
execute_with_retry() {
    local description="$1"
    shift
    local max_retries=3
    local retry=0
    
    while [ $retry -lt $max_retries ]; do
        if [ $retry -gt 0 ]; then
            warning "Retrying $description (attempt $((retry + 1))/$max_retries)..."
            wait_for_transaction "$description"
        fi
        
        log "Executing: $description"
        
        # Capture both stdout and stderr
        local output
        local exit_code=0
        output=$("$@" 2>&1) || exit_code=$?
        
        if [ $exit_code -eq 0 ]; then
            # Check if output indicates we need to wait for mining
            if echo "$output" | grep -q "Waiting for account contract deployment"; then
                success "$description submitted successfully"
                info "Transaction is being mined - this may take several minutes"
                wait_for_transaction "$description"
            else
                success "$description completed successfully"
            fi
            echo "$output"
            return 0
        else
            # Check for specific error patterns
            if echo "$output" | grep -q "Existing nullifier"; then
                warning "Account already deployed (existing nullifier error)"
                success "$description completed (account already exists)"
                echo "$output"
                return 0
                
            elif echo "$output" | grep -q "Timeout awaiting isMined"; then
                local tx_id
                tx_id=$(extract_transaction_id "$output")
                
                warning "Transaction timed out waiting for mining"
                
                local retry_decision
                retry_decision=$(check_and_handle_stale_transaction "$tx_id" "$description")
                
                case $? in
                    0)  # Retry
                        warning "Retrying deployment..."
                        retry=$((retry + 1))
                        continue
                        ;;
                    1)  # Wait and continue
                        info "Continuing with timed-out transaction"
                        echo "$output"
                        return 0
                        ;;
                    2)  # Skip step
                        warning "Skipping $description"
                        return 0
                        ;;
                esac
            fi
            
            error "$description failed"
            retry=$((retry + 1))
            
            if [ $retry -lt $max_retries ]; then
                warning "Command failed, waiting before retry..."
                sleep 30
            else
                error "Output from failed command:"
                echo "$output"
            fi
        fi
    done
    
    error "$description failed after $max_retries attempts"
    return 1
}

# Extract address from command output
extract_address_from_output() {
    local output="$1"
    # Extract the address from "Address: 0x..." line
    echo "$output" | grep "^Address:" | sed 's/Address:[[:space:]]*//' | tr -d ' '
}

# Execute command that depends on previous deployments being mined
execute_with_dependency_retry() {
    local description="$1"
    shift
    local max_retries=5  # More retries for dependency-related commands
    local retry=0
    
    while [ $retry -lt $max_retries ]; do
        if [ $retry -gt 0 ]; then
            warning "Retrying $description - waiting for dependencies (attempt $((retry + 1))/$max_retries)..."
            info "Previous deployments may still be mining or transactions may be stale"
            
            # Longer wait for dependency issues, increasing with each retry
            local wait_time=$((60 + (retry * 30)))
            info "Waiting $wait_time seconds for blockchain state to sync..."
            sleep $wait_time
        fi
        
        log "Executing: $description"
        
        # Capture both stdout and stderr
        local output
        local exit_code=0
        output=$("$@" 2>&1) || exit_code=$?
        
        if [ $exit_code -eq 0 ]; then
            # Check if we got a transaction hash and need to wait for mining
            local tx_hash
            tx_hash=$(echo "$output" | grep "Transaction hash:" | head -1 | sed 's/Transaction hash: //' | tr -d ' ')
            
            if [ -n "$tx_hash" ]; then
                info "Transaction submitted: $tx_hash"
                info "Check status at: http://aztecscan.xyz/tx/$tx_hash"
                
                # Check if transaction was already mined in the output
                if echo "$output" | grep -q "Transaction has been mined"; then
                    if echo "$output" | grep -q "Status: success"; then
                        success "$description completed successfully"
                    else
                        warning "$description transaction mined but check status"
                    fi
                else
                    info "Waiting for transaction to be mined..."
                fi
            else
                success "$description completed successfully"
            fi
            
            echo "$output"
            return 0
        else
            # Check for errors that indicate we need to wait for previous deployments
            if echo "$output" | grep -qi "contract.*not.*found\|contract.*not.*deployed\|account.*not.*found"; then
                warning "Dependency not ready - previous deployment may still be mining"
                retry=$((retry + 1))
                continue
            elif echo "$output" | grep -qi "Cannot find the leaf for nullifier\|nullifier.*not.*found"; then
                warning "Nullifier/state synchronization issue - blockchain state may not be ready"
                info "This often happens when previous transactions are stale or still processing"
                
                if [ $retry -ge 2 ]; then
                    warning "Multiple failures suggest previous transactions may be stale"
                    info "Consider checking transaction status at http://aztecscan.xyz/"
                    echo ""
                    echo "Options:"
                    echo "1. Continue retrying (may work if transactions eventually mine)"
                    echo "2. Exit and manually check/retry stale transactions"
                    echo "3. Skip this step (not recommended)"
                    
                    local choice
                    read -p "Choose option (1/2/3) [default: 1]: " choice
                    choice=${choice:-1}
                    
                    case $choice in
                        2)
                            info "Exiting for manual intervention"
                            exit 1
                            ;;
                        3)
                            warning "Skipping $description"
                            return 0
                            ;;
                    esac
                fi
                
                retry=$((retry + 1))
                continue
            elif echo "$output" | grep -qi "simulation.*failed\|transaction.*simulation.*error"; then
                warning "Transaction simulation failed - dependencies may not be ready"
                retry=$((retry + 1))
                continue
            fi
            
            error "$description failed"
            echo "$output"
            retry=$((retry + 1))
            
            if [ $retry -lt $max_retries ]; then
                warning "Command failed, waiting before retry..."
                sleep 30
            fi
        fi
    done
    
    error "$description failed after $max_retries attempts"
    error "This may indicate that previous deployments are stale or not properly synced"
    info "Check http://aztecscan.xyz/ for deployment status"
    info "You may need to restart deployment with fresh transactions"
    return 1
}

# Set environment variables
setup_environment() {
    log "Setting up environment variables..."
    export NODE_URL="$NODE_URL"
    export SPONSORED_FPC_ADDRESS="$SPONSORED_FPC_ADDRESS"
    export OWNER_SK="$OWNER_SK"
    success "Environment variables set"
}

# Step 2: Create wallets
create_wallets() {
    log "Creating wallets..."
    
    log "Creating owner wallet..."
    local owner_output
    owner_output=$(aztec-wallet create-account \
        -sk "$OWNER_SK" \
        --register-only \
        --node-url "$NODE_URL" \
        --alias owner-wallet 2>&1)
    
    # Extract owner address from output
    OWNER_ADDRESS=$(extract_address_from_output "$owner_output")
    
    if [ -n "$OWNER_ADDRESS" ]; then
        success "Owner wallet created. Address: $OWNER_ADDRESS"
    else
        error "Could not extract owner address from output"
        echo "Owner wallet creation output:"
        echo "$owner_output"
        read -p "Please enter the owner address: " OWNER_ADDRESS
    fi
    
    log "Creating receiver wallet..."
    local receiver_output
    receiver_output=$(aztec-wallet create-account \
        --register-only \
        --node-url "$NODE_URL" \
        --alias receiver-wallet 2>&1)
    
    # Extract receiver address from output  
    local temp_receiver_address
    temp_receiver_address=$(extract_address_from_output "$receiver_output")
    
    if [ -n "$temp_receiver_address" ]; then
        RECEIVER_ADDRESS="$temp_receiver_address"
        success "Receiver wallet created. Address: $RECEIVER_ADDRESS"
    else
        error "Could not extract receiver address from output"
        echo "Receiver wallet creation output:"
        echo "$receiver_output"
        read -p "Please enter the receiver address: " RECEIVER_ADDRESS
    fi
}

# Step 3: Register accounts with FPC
register_with_fpc() {
    log "Registering wallets with FPC..."
    
    execute_with_retry "owner wallet FPC registration" \
        aztec-wallet register-contract \
        --node-url "$NODE_URL" \
        --from owner-wallet \
        --alias sponsoredfpc \
        "$SPONSORED_FPC_ADDRESS" SponsoredFPC \
        --salt 0
    
    execute_with_retry "receiver wallet FPC registration" \
        aztec-wallet register-contract \
        --node-url "$NODE_URL" \
        --from receiver-wallet \
        --alias sponsoredfpc \
        "$SPONSORED_FPC_ADDRESS" SponsoredFPC \
        --salt 0
}

# Step 4: Deploy accounts
deploy_accounts() {
    log "Deploying accounts..."
    warning "Note: 'Timeout awaiting isMined' errors are expected and will be handled"
    
    execute_with_retry "owner wallet deployment" \
        aztec-wallet deploy-account \
        --node-url "$NODE_URL" \
        --from owner-wallet \
        --payment method=fpc-sponsored,fpc=contracts:sponsoredfpc
    
    execute_with_retry "receiver wallet deployment" \
        aztec-wallet deploy-account \
        --node-url "$NODE_URL" \
        --from receiver-wallet \
        --payment method=fpc-sponsored,fpc=contracts:sponsoredfpc
    
    success "Both wallets deployed successfully"
    info "Owner Address: $OWNER_ADDRESS"
    info "Receiver Address: $RECEIVER_ADDRESS"
}

# Step 5: Deploy Token contract
deploy_token_contract() {
    log "Deploying Token contract..."
    
    local token_output
    token_output=$(aztec-wallet deploy \
        --node-url "$NODE_URL" \
        --from accounts:owner-wallet \
        --payment method=fpc-sponsored,fpc=contracts:sponsoredfpc \
        --alias token \
        TokenContract \
        --args accounts:owner-wallet WormToken WORM 18 --no-wait 2>&1)
    
    # Extract token contract address from output
    TOKEN_CONTRACT_ADDRESS=$(echo "$token_output" | grep "Contract deployed at" | sed 's/Contract deployed at //' | tr -d ' ')
    
    if [ -n "$TOKEN_CONTRACT_ADDRESS" ]; then
        success "Token contract deployment initiated. Address: $TOKEN_CONTRACT_ADDRESS"
    else
        warning "Could not extract token contract address from output."
        echo "Token deployment output:"
        echo "$token_output"
        read -p "Please enter the token contract address: " TOKEN_CONTRACT_ADDRESS
    fi
    
    info "Check deployment status at: http://aztecscan.xyz/"
    wait_for_transaction "token contract deployment"
}

# Step 6: Mint tokens
mint_tokens() {
    log "Minting tokens..."
    
    info "Note: Minting may fail initially if previous deployments are still being processed"
    info "The script will automatically retry if needed"
    
    execute_with_dependency_retry "private token minting" \
        aztec-wallet send mint_to_private \
        --node-url "$NODE_URL" \
        --from accounts:owner-wallet \
        --payment method=fpc-sponsored,fpc=contracts:sponsoredfpc \
        --contract-address "$TOKEN_CONTRACT_ADDRESS" \
        --args accounts:owner-wallet accounts:owner-wallet 10000
    
    execute_with_dependency_retry "public token minting" \
        aztec-wallet send mint_to_public \
        --node-url "$NODE_URL" \
        --from accounts:owner-wallet \
        --payment method=fpc-sponsored,fpc=contracts:sponsoredfpc \
        --contract-address "$TOKEN_CONTRACT_ADDRESS" \
        --args accounts:owner-wallet 10000
}

# Backup original contract file
backup_contract() {
    if [ ! -f "$WORMHOLE_CONTRACT_BACKUP" ] && [ -f "$WORMHOLE_CONTRACT_SRC" ]; then
        log "Creating backup of original contract..."
        cp "$WORMHOLE_CONTRACT_SRC" "$WORMHOLE_CONTRACT_BACKUP"
        success "Contract backed up to $WORMHOLE_CONTRACT_BACKUP"
    fi
}

# Restore original contract from backup
restore_contract() {
    if [ -f "$WORMHOLE_CONTRACT_BACKUP" ]; then
        log "Restoring original contract from backup..."
        cp "$WORMHOLE_CONTRACT_BACKUP" "$WORMHOLE_CONTRACT_SRC"
        success "Original contract restored"
    fi
}

# Modify Wormhole contract with captured addresses
modify_wormhole_contract() {
    log "Modifying Wormhole contract with deployment addresses..."
    
    if [ ! -f "$WORMHOLE_CONTRACT_SRC" ]; then
        error "Wormhole contract source file not found: $WORMHOLE_CONTRACT_SRC"
        info "Expected file structure:"
        info "  src/"
        info "  └── main.nr (Wormhole contract)"
        exit 1
    fi
    
    if [ -z "$RECEIVER_ADDRESS" ] || [ -z "$TOKEN_CONTRACT_ADDRESS" ]; then
        error "Missing required addresses for contract modification"
        error "Receiver Address: $RECEIVER_ADDRESS"
        error "Token Contract Address: $TOKEN_CONTRACT_ADDRESS"
        exit 1
    fi
    
    # Create backup first
    backup_contract
    
    info "Updating hardcoded addresses in contract..."
    info "Receiver Address: $RECEIVER_ADDRESS"
    info "Token Contract Address: $TOKEN_CONTRACT_ADDRESS"
    
    # Find and replace the hardcoded addresses in the publish_message_in_private function
    # The current hardcoded addresses that need replacement:
    # receiver_address: 0x2f73c9b19222c2a7931c6cba01eedbbabb51e01b405fe4e0cabe0de91c275d0e
    # token_address: 0x13babb369e8c237a78ed507fe7cc44336a5178ffd02312a979c1fa0921f02a06
    
    local temp_file=$(mktemp)
    
    # Use sed to replace the hardcoded addresses
    sed "s/inner: 0x2f73c9b19222c2a7931c6cba01eedbbabb51e01b405fe4e0cabe0de91c275d0e/inner: $RECEIVER_ADDRESS/g" "$WORMHOLE_CONTRACT_SRC" > "$temp_file" && \
    sed "s/inner: 0x13babb369e8c237a78ed507fe7cc44336a5178ffd02312a979c1fa0921f02a06/inner: $TOKEN_CONTRACT_ADDRESS/g" "$temp_file" > "$WORMHOLE_CONTRACT_SRC"
    
    rm -f "$temp_file"
    
    # Verify the changes were made
    if grep -q "$RECEIVER_ADDRESS" "$WORMHOLE_CONTRACT_SRC" && grep -q "$TOKEN_CONTRACT_ADDRESS" "$WORMHOLE_CONTRACT_SRC"; then
        success "Contract addresses updated successfully"
        info "Receiver address updated in contract"
        info "Token contract address updated in contract"
    else
        error "Failed to update contract addresses"
        warning "Restoring original contract..."
        restore_contract
        exit 1
    fi
}

# Prepare Wormhole contract
prepare_wormhole_contract() {
    log "Preparing Wormhole contract..."
    
    # Modify the contract with the captured addresses
    modify_wormhole_contract
    
    # Compile the contract
    log "Compiling Wormhole contract..."
    if aztec-nargo compile; then
        success "Contract compilation completed successfully"
    else
        error "Contract compilation failed"
        warning "Restoring original contract..."
        restore_contract
        exit 1
    fi
    
    # Run tests
    log "Running contract tests..."
    if aztec test; then
        success "Contract tests passed"
    else
        warning "Contract tests failed - continuing with deployment"
        warning "You may want to review test failures manually"
        
        echo ""
        read -p "Do you want to continue with deployment despite test failures? (y/n): " continue_deploy
        if [[ ! $continue_deploy =~ ^[Yy]$ ]]; then
            info "Deployment cancelled by user"
            warning "Restoring original contract..."
            restore_contract
            exit 1
        fi
    fi
    
    # Verify the compiled contract exists
    if [ ! -f "target/wormhole_contracts-Wormhole.json" ]; then
        error "Compiled Wormhole contract not found at target/wormhole_contracts-Wormhole.json"
        info "Expected compilation output location: target/wormhole_contracts-Wormhole.json"
        warning "Restoring original contract..."
        restore_contract
        exit 1
    fi
    
    success "Wormhole contract prepared successfully"
}

# Step 7: Deploy Wormhole contract
deploy_wormhole_contract() {
    log "Deploying Wormhole contract..."
    
    if [ -z "$RECEIVER_ADDRESS" ] || [ -z "$TOKEN_CONTRACT_ADDRESS" ]; then
        error "Missing required addresses for Wormhole deployment"
        error "Receiver Address: $RECEIVER_ADDRESS"
        error "Token Contract Address: $TOKEN_CONTRACT_ADDRESS"
        exit 1
    fi
    
    local wormhole_output
    wormhole_output=$(aztec-wallet deploy \
        --node-url "$NODE_URL" \
        --from accounts:owner-wallet \
        --payment method=fpc-sponsored,fpc=contracts:sponsoredfpc \
        --alias wormhole \
        target/wormhole_contracts-Wormhole.json \
        --args 56 56 "$RECEIVER_ADDRESS" "$TOKEN_CONTRACT_ADDRESS" --no-wait --init init 2>&1)
    
    # Extract Wormhole contract address from output
    WORMHOLE_CONTRACT_ADDRESS=$(echo "$wormhole_output" | grep "Contract deployed at" | sed 's/Contract deployed at //' | tr -d ' ')
    
    if [ -n "$WORMHOLE_CONTRACT_ADDRESS" ]; then
        success "Wormhole contract deployment initiated. Address: $WORMHOLE_CONTRACT_ADDRESS"
    else
        warning "Could not extract Wormhole contract address from output."
        echo "Wormhole deployment output:"
        echo "$wormhole_output"
        read -p "Please enter the Wormhole contract address: " WORMHOLE_CONTRACT_ADDRESS
    fi
    
    info "Check deployment status at: http://aztecscan.xyz/"
    wait_for_transaction "Wormhole contract deployment"
    
    # Restore original contract after successful deployment
    log "Restoring original contract file..."
    restore_contract
}

# Cleanup function
cleanup_on_exit() {
    warning "Script interrupted - cleaning up..."
    if [ -f "$WORMHOLE_CONTRACT_BACKUP" ]; then
        log "Restoring original contract..."
        restore_contract
        rm -f "$WORMHOLE_CONTRACT_BACKUP"
    fi
    exit 1
}

# Main execution function
main() {
    setup_wizard
    check_dependencies
    setup_environment
    
    log "Starting deployment process..."
    
    create_wallets
    register_with_fpc
    deploy_accounts
    deploy_token_contract
    mint_tokens
    prepare_wormhole_contract
    deploy_wormhole_contract
    
    success "🎉 Deployment completed successfully!"
    echo -e "\n${CYAN}Final Deployment Summary:${NC}"
    echo "├─ Node URL: $NODE_URL"
    echo "├─ Owner Wallet: $OWNER_ADDRESS"
    echo "├─ Receiver Wallet: $RECEIVER_ADDRESS"
    echo "├─ Token Contract: $TOKEN_CONTRACT_ADDRESS"
    echo "├─ Wormhole Contract: $WORMHOLE_CONTRACT_ADDRESS"
    echo "└─ Transaction Explorer: http://aztecscan.xyz/"
    echo ""
    success "All contracts deployed and ready for use!"
    
    # Clean up backup file
    if [ -f "$WORMHOLE_CONTRACT_BACKUP" ]; then
        rm -f "$WORMHOLE_CONTRACT_BACKUP"
        info "Cleanup completed"
    fi
}

# Handle script interruption and cleanup
trap cleanup_on_exit SIGINT SIGTERM

# Run the script
main "$@"