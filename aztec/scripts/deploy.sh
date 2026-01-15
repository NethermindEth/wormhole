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
DEFAULT_NODE_URL="https://next.devnet.aztec-labs.com/"
DEFAULT_SPONSORED_FPC_ADDRESS="0x1586f476995be97f07ebd415340a14be48dc28c6c661cc6bdddb80ae790caa4e"
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

# Contract file paths (relative to aztec directory)
WORMHOLE_CONTRACT_TARGET="contracts/target/wormhole_contracts-Wormhole.json"

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
    
    warning "Important: Default Private Key Usage"
    if [[ "$OWNER_SK" == "$DEFAULT_OWNER_SK" ]]; then
        echo -e "${YELLOW}You are using the default owner private key.${NC}"
        echo ""
        echo -e "${CYAN}What this means:${NC}"
        echo "• This private key may have been used before on this network"
        echo "• If the account is already deployed, you'll see 'Existing nullifier' error"
        echo "• This is NORMAL and means your account is already ready to use"
        echo "• The script will detect this and continue successfully"
        echo ""
        echo -e "${CYAN}Why this happens:${NC}"
        echo "• Aztec uses nullifiers to prevent double-spending"
        echo "• Each account deployment creates a unique nullifier"
        echo "• Trying to deploy the same account twice triggers this protection"
        echo "• The error actually confirms your account exists and is secure"
        echo ""
    else
        echo -e "${GREEN}You are using a custom private key.${NC}"
        echo "• If this is a new key, the account will be deployed fresh"
        echo "• If you've used this key before, you may see 'Existing nullifier' error"
        echo "• Either way, the script will handle it correctly"
        echo ""
    fi
    
    read -p "Press Enter to continue with deployment..."
}

# Check dependencies
check_dependencies() {
    log "Checking dependencies..."

    # In Aztec 3.0.0+, all tools are subcommands of the main `aztec` CLI
    if ! command -v aztec &> /dev/null; then
        error "Missing dependency: aztec"
        error "Please install Aztec CLI tools before continuing."
        info "Installation instructions:"
        info "Install Aztec CLI: https://docs.aztec.network/getting_started"
        exit 1
    fi

    success "All dependencies are installed"

    # Display version for debugging
    info "Aztec CLI version: $(aztec --version 2>/dev/null || echo 'version unknown')"

    # Fetch the canonical FPC address for the current CLI version
    log "Fetching canonical SponsoredFPC address..."
    local fpc_output
    fpc_output=$(aztec get-canonical-sponsored-fpc-address 2>&1)
    local canonical_fpc
    canonical_fpc=$(echo "$fpc_output" | grep "Canonical SponsoredFPC Address:" | sed 's/Canonical SponsoredFPC Address: //' | tr -d ' ')

    if [ -n "$canonical_fpc" ]; then
        DEFAULT_SPONSORED_FPC_ADDRESS="$canonical_fpc"
        success "Using canonical FPC address: $canonical_fpc"
    else
        warning "Could not fetch canonical FPC address, using default"
    fi
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
    
    info "Transaction submitted for $description"
    info "Aztec transactions are processed automatically - continuing with deployment"
    info "You can monitor all transactions at: http://aztecscan.xyz/"
    
    # Short pause to allow transaction to propagate
    sleep 5
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
            # Extract contract address if this is a deployment
            if [[ "$description" == *"deployment"* ]]; then
                local contract_address
                contract_address=$(echo "$output" | grep "Contract deployed at" | sed 's/Contract deployed at //' | tr -d ' ')
                
                if [ -n "$contract_address" ]; then
                    if [[ "$description" == *"Token"* ]]; then
                        TOKEN_CONTRACT_ADDRESS="$contract_address"
                        success "Token contract deployed at: $TOKEN_CONTRACT_ADDRESS"
                    elif [[ "$description" == *"Wormhole"* ]]; then
                        WORMHOLE_CONTRACT_ADDRESS="$contract_address"
                        success "Wormhole contract deployed at: $WORMHOLE_CONTRACT_ADDRESS"
                    fi
                fi
            fi
            
            # Check if we got a transaction hash and need to wait for mining
            local tx_hash
            tx_hash=$(echo "$output" | grep "Deploy tx hash:" | head -1 | sed 's/Deploy tx hash:[[:space:]]*//' | tr -d ' ')
            
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
                    info "Transaction deployment initiated - continuing with next step"
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

# Step 2: Register FPC contract (must be done before creating accounts with sponsored payment)
register_fpc() {
    log "Registering FPC contract in wallet..."

    # Register the FPC contract in the wallet's PXE (per devnet docs)
    execute_with_retry "FPC contract registration" \
        aztec-wallet register-contract \
        --node-url "$NODE_URL" \
        --alias sponsoredfpc \
        "$SPONSORED_FPC_ADDRESS" SponsoredFPC \
        --salt 0
}

# Step 3: Create and deploy wallets (combined per devnet docs)
create_and_deploy_wallets() {
    log "Creating and deploying wallets..."
    warning "Note: 'Existing nullifier' errors indicate accounts are already deployed"
    info "This is expected when reusing the same private keys and will be handled automatically"

    log "Creating and deploying owner wallet..."
    local owner_output
    owner_output=$(execute_with_retry "owner wallet creation" \
        aztec-wallet create-account \
        --node-url "$NODE_URL" \
        --alias owner-wallet \
        --secret-key "$OWNER_SK" \
        --payment method=fpc-sponsored,fpc="$SPONSORED_FPC_ADDRESS" 2>&1) || true

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

    log "Creating and deploying receiver wallet..."
    local receiver_output
    receiver_output=$(execute_with_retry "receiver wallet creation" \
        aztec-wallet create-account \
        --node-url "$NODE_URL" \
        --alias receiver-wallet \
        --payment method=fpc-sponsored,fpc="$SPONSORED_FPC_ADDRESS" 2>&1) || true

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

    success "Account creation and deployment completed"
    info "Owner Address: $OWNER_ADDRESS"
    info "Receiver Address: $RECEIVER_ADDRESS"
}

# Step 4: Deploy Token contract
deploy_token_contract() {
    log "Deploying Token contract..."

    execute_with_dependency_retry "Token contract deployment" \
        aztec-wallet deploy \
        --node-url "$NODE_URL" \
        --from accounts:owner-wallet \
        --payment method=fpc-sponsored,fpc="$SPONSORED_FPC_ADDRESS" \
        --alias token \
        TokenContract \
        --args accounts:owner-wallet WormToken WORM 18 --no-wait
}

# Step 5: Mint tokens
mint_tokens() {
    log "Minting tokens..."

    info "Note: Minting may fail initially if previous deployments are still being processed"
    info "The script will automatically retry if needed"

    execute_with_dependency_retry "private token minting" \
        aztec-wallet send mint_to_private \
        --node-url "$NODE_URL" \
        --from accounts:owner-wallet \
        --payment method=fpc-sponsored,fpc="$SPONSORED_FPC_ADDRESS" \
        --contract-address token \
        --args accounts:owner-wallet 10000

    execute_with_dependency_retry "public token minting" \
        aztec-wallet send mint_to_public \
        --node-url "$NODE_URL" \
        --from accounts:owner-wallet \
        --payment method=fpc-sponsored,fpc="$SPONSORED_FPC_ADDRESS" \
        --contract-address token \
        --args accounts:owner-wallet 10000
}


# Prepare Wormhole contract (compile and test)
prepare_wormhole_contract() {
    log "Preparing Wormhole contract..."

    # Addresses are passed as constructor args, no source modification needed
    info "Receiver Address: $RECEIVER_ADDRESS"
    info "Token Contract Address: $TOKEN_CONTRACT_ADDRESS"

    # Compile the contract with retry logic
    compile_contract_with_retry

    # Run tests with retry logic
    test_contract_with_retry

    # Verify the compiled contract exists
    if [ ! -f "$WORMHOLE_CONTRACT_TARGET" ]; then
        error "Compiled Wormhole contract not found at $WORMHOLE_CONTRACT_TARGET"
        info "Expected compilation output location: $WORMHOLE_CONTRACT_TARGET"
        exit 1
    fi

    success "Wormhole contract prepared successfully"
}

# Compile contract with retry logic
compile_contract_with_retry() {
    local max_attempts=3
    local attempt=1
    
    while [ $attempt -le $max_attempts ]; do
        log "Compiling Wormhole contract (attempt $attempt/$max_attempts)..."

        local compile_output
        local compile_exit_code=0
        # aztec compile handles both nargo compile and postprocessing
        # Run from contracts directory
        compile_output=$(cd contracts && aztec compile 2>&1) || compile_exit_code=$?

        if [ $compile_exit_code -eq 0 ]; then
            success "Contract compilation completed successfully"
            echo "$compile_output"
            return 0
        else
            error "Contract compilation failed (attempt $attempt/$max_attempts)"
            echo "Compilation output:"
            echo "$compile_output"
            echo ""
            
            if [ $attempt -lt $max_attempts ]; then
                warning "Compilation failed - this might happen if:"
                echo "• The contract file is being modified while the script runs"
                echo "• There are temporary file system issues"
                echo "• The contract has syntax errors that were just introduced"
                echo ""
                
                echo "Options:"
                echo "1. Retry compilation (recommended if file was being edited)"
                echo "2. Exit and fix compilation errors manually" 
                echo "3. Skip compilation and try with existing artifacts (risky)"
                
                local choice
                read -p "Choose option (1/2/3) [default: 1]: " choice
                choice=${choice:-1}
                
                case $choice in
                    1)
                        info "Retrying compilation..."
                        if [ $attempt -eq 1 ]; then
                            info "Waiting 10 seconds in case files are still being modified..."
                            sleep 10
                        else
                            info "Waiting 5 seconds before retry..."
                            sleep 5
                        fi
                        attempt=$((attempt + 1))
                        continue
                        ;;
                    2)
                        info "Exiting for manual compilation fix"
                        exit 1
                        ;;
                    3)
                        warning "Skipping compilation - using existing artifacts"
                        warning "This may cause deployment failures if artifacts are outdated"
                        return 0
                        ;;
                    *)
                        info "Invalid choice, defaulting to retry"
                        attempt=$((attempt + 1))
                        continue
                        ;;
                esac
            else
                error "Compilation failed after $max_attempts attempts"
                
                echo ""
                echo "Compilation has failed multiple times. This usually means:"
                echo "• There are syntax errors in the contract"
                echo "• Missing dependencies or incorrect paths"
                echo "• The contract modifications introduced errors"
                echo ""
                echo "Please check the compilation errors above and fix them manually."
                echo "You can then run the script again or compile manually with:"
                echo "  cd contracts && aztec compile"
                exit 1
            fi
        fi
    done
}

# Test contract with retry logic  
test_contract_with_retry() {
    local max_attempts=2
    local attempt=1
    
    while [ $attempt -le $max_attempts ]; do
        log "Running contract tests (attempt $attempt/$max_attempts)..."
        
        local test_output
        local test_exit_code=0
        # Run from contracts directory
        test_output=$(cd contracts && aztec test 2>&1) || test_exit_code=$?
        
        if [ $test_exit_code -eq 0 ]; then
            success "Contract tests passed"
            echo "$test_output"
            return 0
        else
            warning "Contract tests failed (attempt $attempt/$max_attempts)"
            echo "Test output:"
            echo "$test_output"
            echo ""
            
            if [ $attempt -lt $max_attempts ]; then
                warning "Tests failed - this might happen if:"
                echo "• Contract was recently compiled and test cache is stale"
                echo "• Temporary testing environment issues"
                echo "• Tests depend on external state that's not ready"
                echo ""
                
                echo "Options:"
                echo "1. Retry tests (recommended for transient issues)"
                echo "2. Continue with deployment despite test failures (risky)"
                echo "3. Exit and fix test failures manually"
                
                local choice
                read -p "Choose option (1/2/3) [default: 1]: " choice
                choice=${choice:-1}
                
                case $choice in
                    1)
                        info "Retrying tests..."
                        info "Waiting 5 seconds for test environment to stabilize..."
                        sleep 5
                        attempt=$((attempt + 1))
                        continue
                        ;;
                    2)
                        warning "Continuing with deployment despite test failures"
                        warning "Deployment may fail if tests revealed actual issues"
                        return 0
                        ;;
                    3)
                        info "Exiting for manual test fix"
                        exit 1
                        ;;
                    *)
                        info "Invalid choice, defaulting to retry"
                        attempt=$((attempt + 1))
                        continue
                        ;;
                esac
            else
                warning "Tests failed after $max_attempts attempts"
                
                echo ""
                echo "Do you want to continue with deployment despite test failures?"
                echo "This is risky but sometimes tests fail due to environment issues"
                echo "while the actual contract functionality is correct."
                
                local continue_deploy
                read -p "Continue with deployment? (y/n) [default: n]: " continue_deploy
                continue_deploy=${continue_deploy:-n}
                
                if [[ $continue_deploy =~ ^[Yy]$ ]]; then
                    warning "Continuing with deployment despite test failures"
                    warning "Monitor deployment carefully for any issues"
                    return 0
                else
                    info "Deployment cancelled by user"
                    exit 1
                fi
            fi
        fi
    done
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
    
    execute_with_dependency_retry "Wormhole contract deployment" \
        aztec-wallet deploy \
        --node-url "$NODE_URL" \
        --from accounts:owner-wallet \
        --payment method=fpc-sponsored,fpc="$SPONSORED_FPC_ADDRESS" \
        --alias wormhole \
        "$WORMHOLE_CONTRACT_TARGET" \
        --args 56 56 accounts:owner-wallet "$RECEIVER_ADDRESS" "$TOKEN_CONTRACT_ADDRESS" --no-wait --init init
}

# Create environment file for verification service
create_env_file() {
    local env_file=".env"
    
    log "Creating environment file for verification service..."
    
    # Create or overwrite .env file
    cat > "$env_file" << EOF
# Aztec Deployment Configuration
# Generated on $(date)

# Network Configuration
NODE_URL=$NODE_URL
PRIVATE_KEY=$OWNER_SK
SALT=0x0000000000000000000000000000000000000000000000000000000000000000

# Contract Addresses
OWNER_ADDRESS=$OWNER_ADDRESS
RECEIVER_ADDRESS=$RECEIVER_ADDRESS
TOKEN_CONTRACT_ADDRESS=$TOKEN_CONTRACT_ADDRESS
WORMHOLE_CONTRACT_ADDRESS=$WORMHOLE_CONTRACT_ADDRESS

# Service Configuration
PORT=3000
NETWORK=testnet
EOF
    
    success "Environment file created: $env_file"
    info "The verification service will now use these deployed contract addresses"
}

# Export environment variables for immediate use
export_environment_variables() {
    log "Exporting environment variables for verification service..."
    
    export NODE_URL="$NODE_URL"
    export PRIVATE_KEY="$OWNER_SK"
    export CONTRACT_ADDRESS="$WORMHOLE_CONTRACT_ADDRESS"
    export SALT="0x0000000000000000000000000000000000000000000000000000000000000000"
    export OWNER_ADDRESS="$OWNER_ADDRESS"
    export RECEIVER_ADDRESS="$RECEIVER_ADDRESS"
    export TOKEN_CONTRACT_ADDRESS="$TOKEN_CONTRACT_ADDRESS"
    export WORMHOLE_CONTRACT_ADDRESS="$WORMHOLE_CONTRACT_ADDRESS"
    export PORT="3000"
    export NETWORK="testnet"
    
    success "Environment variables exported for current session"
}

# Cleanup function
cleanup_on_exit() {
    warning "Script interrupted - cleaning up..."
    exit 1
}

# Main execution function
main() {
    check_dependencies
    setup_wizard
    setup_environment
    
    log "Starting deployment process..."

    register_fpc
    create_and_deploy_wallets
    deploy_token_contract

    # Add a pause to let the token contract deploy before minting
    info "Waiting for token contract to be ready before minting..."
    sleep 30

    mint_tokens
    prepare_wormhole_contract
    deploy_wormhole_contract

    success "Deployment completed successfully!"
    echo -e "\n${CYAN}Final Deployment Summary:${NC}"
    echo "├─ Node URL: $NODE_URL"
    echo "├─ Owner Wallet: $OWNER_ADDRESS"
    echo "├─ Receiver Wallet: $RECEIVER_ADDRESS"
    echo "├─ Token Contract: $TOKEN_CONTRACT_ADDRESS"

    if [ -n "$WORMHOLE_CONTRACT_ADDRESS" ]; then
        echo "├─ Wormhole Contract: $WORMHOLE_CONTRACT_ADDRESS"
    else
        echo "├─ Wormhole Contract: Deployed (check devnet.aztecscan.xyz for address)"
    fi

    echo "└─ Transaction Explorer: https://devnet.aztecscan.xyz"
    echo ""
    success "All contracts deployed and ready for use!"
    info "Note: Contract addresses may take a few minutes to be fully propagated"
    
    # Create environment file and export variables
    create_env_file
    export_environment_variables
}

# Handle script interruption and cleanup
trap cleanup_on_exit SIGINT SIGTERM

# Run the script
main "$@"