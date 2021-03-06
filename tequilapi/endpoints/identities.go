/*
 * Copyright (C) 2017 The "MysteriumNetwork/node" Authors.
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

package endpoints

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"

	"github.com/ethereum/go-ethereum/common"
	"github.com/julienschmidt/httprouter"
	"github.com/mysteriumnetwork/node/config"
	"github.com/mysteriumnetwork/node/identity"
	"github.com/mysteriumnetwork/node/identity/registry"
	identity_selector "github.com/mysteriumnetwork/node/identity/selector"
	"github.com/mysteriumnetwork/node/session/pingpong"
	pingpong_event "github.com/mysteriumnetwork/node/session/pingpong/event"
	"github.com/mysteriumnetwork/node/tequilapi/contract"
	"github.com/mysteriumnetwork/node/tequilapi/utils"
	"github.com/mysteriumnetwork/payments/client"
	"github.com/pkg/errors"
)

type balanceProvider interface {
	ForceBalanceUpdate(chainID int64, id identity.Identity) *big.Int
}

type earningsProvider interface {
	GetEarnings(chainID int64, id identity.Identity) pingpong_event.Earnings
}

type beneficiaryProvider interface {
	GetBeneficiary(identity common.Address) (common.Address, error)
}

type providerChannel interface {
	GetProviderChannel(chainID int64, hermesAddress common.Address, provider common.Address, pending bool) (client.ProviderChannel, error)
}

type identitiesAPI struct {
	idm               identity.Manager
	selector          identity_selector.Handler
	registry          registry.IdentityRegistry
	channelCalculator *pingpong.AddressProvider
	balanceProvider   balanceProvider
	earningsProvider  earningsProvider
	bc                providerChannel
	transactor        Transactor
	bprovider         beneficiaryProvider
}

// swagger:operation GET /identities Identity listIdentities
// ---
// summary: Returns identities
// description: Returns list of identities
// responses:
//   200:
//     description: List of identities
//     schema:
//       "$ref": "#/definitions/ListIdentitiesResponse"
//   500:
//     description: Internal server error
//     schema:
//       "$ref": "#/definitions/ErrorMessageDTO"
func (endpoint *identitiesAPI) List(resp http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	ids := endpoint.idm.GetIdentities()
	idsDTO := contract.NewIdentityListResponse(ids)
	utils.WriteAsJSON(idsDTO, resp)
}

// swagger:operation PUT /identities/current Identity currentIdentity
// ---
// summary: Returns my current identity
// description: Tries to retrieve the last used identity, the first identity, or creates and returns a new identity
// parameters:
//   - in: body
//     name: body
//     description: Parameter in body (passphrase) required for creating new identity
//     schema:
//       $ref: "#/definitions/IdentityCurrentRequestDTO"
// responses:
//   200:
//     description: Unlocked identity returned
//     schema:
//       "$ref": "#/definitions/IdentityRefDTO"
//   400:
//     description: Bad Request
//     schema:
//       "$ref": "#/definitions/ErrorMessageDTO"
//   422:
//     description: Parameters validation error
//     schema:
//       "$ref": "#/definitions/ValidationErrorDTO"
//   500:
//     description: Internal server error
//     schema:
//       "$ref": "#/definitions/ErrorMessageDTO"
func (endpoint *identitiesAPI) Current(resp http.ResponseWriter, request *http.Request, params httprouter.Params) {
	var req contract.IdentityCurrentRequest
	err := json.NewDecoder(request.Body).Decode(&req)
	if err != nil {
		utils.SendError(resp, err, http.StatusBadRequest)
		return
	}

	if errorMap := req.Validate(); errorMap.HasErrors() {
		utils.SendValidationErrorMessage(resp, errorMap)
		return
	}

	idAddress := ""
	if req.Address != nil {
		idAddress = *req.Address
	}

	chainID := config.GetInt64(config.FlagChainID)
	id, err := endpoint.selector.UseOrCreate(idAddress, *req.Passphrase, chainID)

	if err != nil {
		utils.SendError(resp, err, http.StatusInternalServerError)
		return
	}

	idDTO := contract.NewIdentityDTO(id)
	utils.WriteAsJSON(idDTO, resp)
}

// swagger:operation POST /identities Identity createIdentity
// ---
// summary: Creates new identity
// description: Creates identity and stores in keystore encrypted with passphrase
// parameters:
//   - in: body
//     name: body
//     description: Parameter in body (passphrase) required for creating new identity
//     schema:
//       $ref: "#/definitions/IdentityCreateRequestDTO"
// responses:
//   200:
//     description: Identity created
//     schema:
//       "$ref": "#/definitions/IdentityRefDTO"
//   400:
//     description: Bad Request
//     schema:
//       "$ref": "#/definitions/ErrorMessageDTO"
//   422:
//     description: Parameters validation error
//     schema:
//       "$ref": "#/definitions/ValidationErrorDTO"
//   500:
//     description: Internal server error
//     schema:
//       "$ref": "#/definitions/ErrorMessageDTO"
func (endpoint *identitiesAPI) Create(resp http.ResponseWriter, httpReq *http.Request, _ httprouter.Params) {
	var req contract.IdentityCreateRequest
	err := json.NewDecoder(httpReq.Body).Decode(&req)
	if err != nil {
		utils.SendError(resp, err, http.StatusBadRequest)
		return
	}

	if errorMap := req.Validate(); errorMap.HasErrors() {
		utils.SendValidationErrorMessage(resp, errorMap)
		return
	}

	id, err := endpoint.idm.CreateNewIdentity(*req.Passphrase)
	if err != nil {
		utils.SendError(resp, err, http.StatusInternalServerError)
		return
	}

	idDTO := contract.NewIdentityDTO(id)
	utils.WriteAsJSON(idDTO, resp)
}

// swagger:operation PUT /identities/{id}/unlock Identity unlockIdentity
// ---
// summary: Unlocks identity
// description: Uses passphrase to decrypt identity stored in keystore
// parameters:
// - in: path
//   name: id
//   description: Identity stored in keystore
//   type: string
//   required: true
// - in: body
//   name: body
//   description: Parameter in body (passphrase) required for unlocking identity
//   schema:
//     $ref: "#/definitions/IdentityUnlockRequestDTO"
// responses:
//   202:
//     description: Identity unlocked
//   400:
//     description: Body parsing error
//     schema:
//       "$ref": "#/definitions/ErrorMessageDTO"
//   403:
//     description: Forbidden
//     schema:
//       "$ref": "#/definitions/ErrorMessageDTO"
//   500:
//     description: Internal server error
//     schema:
//       "$ref": "#/definitions/ErrorMessageDTO"
func (endpoint *identitiesAPI) Unlock(resp http.ResponseWriter, httpReq *http.Request, params httprouter.Params) {
	address := params.ByName("id")
	id, err := endpoint.idm.GetIdentity(address)
	if err != nil {
		utils.SendError(resp, err, http.StatusNotFound)
		return
	}

	var req contract.IdentityUnlockRequest
	err = json.NewDecoder(httpReq.Body).Decode(&req)
	if err != nil {
		utils.SendError(resp, err, http.StatusBadRequest)
		return
	}

	if errorMap := req.Validate(); errorMap.HasErrors() {
		utils.SendValidationErrorMessage(resp, errorMap)
		return
	}

	chainID := config.GetInt64(config.FlagChainID)
	err = endpoint.idm.Unlock(chainID, id.Address, *req.Passphrase)
	if err != nil {
		utils.SendError(resp, err, http.StatusForbidden)
		return
	}
	resp.WriteHeader(http.StatusAccepted)
}

// swagger:operation GET /identities/{id} Identity getIdentity
// ---
// summary: Get identity
// description: Provide identity details
// parameters:
//   - in: path
//     name: id
//     description: hex address of identity
//     type: string
//     required: true
// responses:
//   200:
//     description: Identity retrieved
//     schema:
//       "$ref": "#/definitions/IdentityRefDTO"
//   500:
//     description: Internal server error
//     schema:
//       "$ref": "#/definitions/ErrorMessageDTO"
func (endpoint *identitiesAPI) Get(resp http.ResponseWriter, _ *http.Request, params httprouter.Params) {
	address := params.ByName("id")
	id, err := endpoint.idm.GetIdentity(address)
	if err != nil {
		utils.SendError(resp, err, http.StatusNotFound)
		return
	}

	chainID := config.GetInt64(config.FlagChainID)
	regStatus, err := endpoint.registry.GetRegistrationStatus(chainID, id)
	if err != nil {
		utils.SendError(resp, errors.Wrap(err, "failed to check identity registration status"), http.StatusInternalServerError)
		return
	}

	channelAddress, err := endpoint.channelCalculator.GetChannelAddress(chainID, id)
	if err != nil {
		utils.SendError(resp, fmt.Errorf("failed to calculate channel address %w", err), http.StatusInternalServerError)
		return
	}

	var stake = new(big.Int)
	if regStatus == registry.Registered {
		hermesID, err := endpoint.channelCalculator.GetActiveHermes(chainID)
		if err != nil {
			utils.SendError(resp, fmt.Errorf("could not get active hermes %w", err), http.StatusInternalServerError)
			return
		}

		data, err := endpoint.bc.GetProviderChannel(chainID, hermesID, common.HexToAddress(address), false)
		if err != nil {
			utils.SendError(resp, fmt.Errorf("failed to check identity registration status: %w", err), http.StatusInternalServerError)
			return
		}
		stake = data.Stake
	}

	balance := endpoint.balanceProvider.ForceBalanceUpdate(chainID, id)
	settlement := endpoint.earningsProvider.GetEarnings(chainID, id)
	status := contract.IdentityDTO{
		Address:            address,
		RegistrationStatus: regStatus.String(),
		ChannelAddress:     channelAddress.Hex(),
		Balance:            balance,
		Earnings:           settlement.UnsettledBalance,
		EarningsTotal:      settlement.LifetimeBalance,
		Stake:              stake,
	}
	utils.WriteAsJSON(status, resp)
}

// swagger:operation GET /identities/{id}/registration Identity identityRegistration
// ---
// summary: Provide identity registration status
// description: Provides registration status for given identity, if identity is not registered - provides additional data required for identity registration
// parameters:
//   - in: path
//     name: id
//     description: hex address of identity
//     type: string
//     required: true
// responses:
//   200:
//     description: Status retrieved
//     schema:
//       "$ref": "#/definitions/IdentityRegistrationResponseDTO"
//   500:
//     description: Internal server error
//     schema:
//       "$ref": "#/definitions/ErrorMessageDTO"
func (endpoint *identitiesAPI) RegistrationStatus(resp http.ResponseWriter, _ *http.Request, params httprouter.Params) {
	address := params.ByName("id")
	id, err := endpoint.idm.GetIdentity(address)
	if err != nil {
		utils.SendError(resp, err, http.StatusNotFound)
		return
	}

	regStatus, err := endpoint.registry.GetRegistrationStatus(config.GetInt64(config.FlagChainID), id)
	if err != nil {
		utils.SendError(resp, errors.Wrap(err, "failed to check identity registration status"), http.StatusInternalServerError)
		return
	}

	registrationDataDTO := &contract.IdentityRegistrationResponse{
		Status:     regStatus.String(),
		Registered: regStatus.Registered(),
	}
	utils.WriteAsJSON(registrationDataDTO, resp)
}

// swagger:operation GET /identities/{id}/beneficiary Identity beneficiary address
// ---
// summary: Provide identity beneficiary address
// description: Provides beneficiary address for given identity
// parameters:
//   - in: path
//     name: id
//     description: hex address of identity
//     type: string
//     required: true
// responses:
//   200:
//     description: Beneficiary retrieved
//     schema:
//       "$ref": "#/definitions/IdentityBeneficiaryResponseDTO"
//   500:
//     description: Internal server error
//     schema:
//       "$ref": "#/definitions/ErrorMessageDTO"
func (endpoint *identitiesAPI) Beneficiary(resp http.ResponseWriter, _ *http.Request, params httprouter.Params) {
	address := params.ByName("id")
	data, err := endpoint.bprovider.GetBeneficiary(common.HexToAddress(address))
	if err != nil {
		utils.SendError(resp, fmt.Errorf("failed to check identity registration status: %w", err), http.StatusInternalServerError)
		return
	}

	registrationDataDTO := &contract.IdentityBeneficiaryResponse{
		Beneficiary: data.Hex(),
	}
	utils.WriteAsJSON(registrationDataDTO, resp)
}

// swagger:operation GET /identities/{id}/referral Referral
// ---
// summary: Gets referral token
// description: Gets a referral token for the given identity if a campaign exists
// parameters:
// - name: id
//   in: path
//   description: Identity for which to get a token
//   type: string
//   required: true
// responses:
//   200:
//     description: Token response
//   500:
//     description: Internal server error
//     schema:
//       "$ref": "#/definitions/ErrorMessageDTO"
func (endpoint *identitiesAPI) GetReferralToken(resp http.ResponseWriter, request *http.Request, params httprouter.Params) {
	id := params.ByName("id")
	tkn, err := endpoint.transactor.GetReferralToken(common.HexToAddress(id))
	if err != nil {
		utils.SendError(resp, err, http.StatusInternalServerError)
		return
	}
	utils.WriteAsJSON(contract.ReferralTokenResponse{
		Token: tkn,
	}, resp)
}

// swagger:operation GET /identities/{id}/referral-available Referral availability check
// ---
// summary: Checks if the user can obtain a referral token
// description: Verifies user's eligibility and the presence of an applicable public campaign
// parameters:
// - name: id
//   in: path
//   description: Identity for which to get a token
//   type: string
//   required: true
// responses:
//   200:
//     description: Success
//   500:
//     description: Internal server error
//     schema:
//       "$ref": "#/definitions/ErrorMessageDTO"
func (endpoint *identitiesAPI) ReferralTokenAvailable(resp http.ResponseWriter, request *http.Request, params httprouter.Params) {
	id := params.ByName("id")
	err := endpoint.transactor.ReferralTokenAvailable(common.HexToAddress(id))
	if err != nil {
		utils.SendError(resp, err, http.StatusInternalServerError)
		return
	}
}

// AddRoutesForIdentities creates /identities endpoint on tequilapi service
func AddRoutesForIdentities(
	router *httprouter.Router,
	idm identity.Manager,
	selector identity_selector.Handler,
	registry registry.IdentityRegistry,
	balanceProvider balanceProvider,
	channelAddressCalculator *pingpong.AddressProvider,
	earningsProvider earningsProvider,
	bc providerChannel,
	transactor Transactor,
	bprovider beneficiaryProvider,
) {
	idmEnd := &identitiesAPI{
		idm:               idm,
		selector:          selector,
		registry:          registry,
		balanceProvider:   balanceProvider,
		channelCalculator: channelAddressCalculator,
		earningsProvider:  earningsProvider,
		bc:                bc,
		transactor:        transactor,
		bprovider:         bprovider,
	}
	router.GET("/identities", idmEnd.List)
	router.POST("/identities", idmEnd.Create)
	router.PUT("/identities/:id", func(resp http.ResponseWriter, request *http.Request, params httprouter.Params) {
		// TODO: remove this hack when we replace our router
		switch params.ByName("id") {
		case "current":
			idmEnd.Current(resp, request, params)
		default:
			http.NotFound(resp, request)
		}
	})
	router.GET("/identities/:id", idmEnd.Get)
	router.GET("/identities/:id/status", idmEnd.Get)
	router.PUT("/identities/:id/unlock", idmEnd.Unlock)
	router.GET("/identities/:id/registration", idmEnd.RegistrationStatus)
	router.GET("/identities/:id/beneficiary", idmEnd.Beneficiary)
	router.GET("/identities/:id/referral", idmEnd.GetReferralToken)
	router.GET("/identities/:id/referral-available", idmEnd.ReferralTokenAvailable)
}
